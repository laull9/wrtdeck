package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// marker_name 是导出目录里的指纹文件，记录当前落盘的是哪一版前端资源。
// 有了它，每次启动导出都能先比指纹再决定要不要写盘，避免反复磨损闪存。
const marker_name = ".wrtdeck-web"

// skip_names 是不参与指纹与导出的文件名。
//   - .DS_Store 是 macOS 的目录元数据，在各次构建之间会变化，
//     把它算进指纹会导致「什么都没改却每次都判定需要重新导出」；
//   - .gitkeep 只是为了让空构建目录能被 embed 收录，设备上不需要它。
var skip_names = map[string]bool{".DS_Store": true, ".gitkeep": true}

// ExportResult 描述一次导出的结果
type ExportResult struct {
	Dir         string
	Fingerprint string
	Files       int
	Changed     bool
}

// fingerprint_once 让指纹只算一次，进程生命周期内不再重复读整个资源树
var fingerprint_once = sync.OnceValue(compute_fingerprint)

// Fingerprint 返回内嵌前端资源的整体指纹，资源不可读时返回空串
func Fingerprint() string {
	return fingerprint_once()
}

// Export 把内嵌的前端资源导出到 dir，供设备上的 Web 服务器直接服务。
//
// 面板本体自己也能提供这些文件，但只有落到文件系统上，uhttpd 之类的
// 静态服务器才能用零开销的方式把它们和 LuCI 放在同一个源上。
// 目录里已是最新版本时不写任何文件，返回的 Changed 为 false。
func Export(dir string) (ExportResult, error) {
	result := ExportResult{Dir: dir, Fingerprint: Fingerprint()}
	if result.Fingerprint == "" {
		return result, fmt.Errorf("内嵌前端资源不可用，无法导出")
	}
	if dir == "" || dir == "/" || !filepath.IsAbs(dir) {
		return result, fmt.Errorf("导出目录必须是绝对路径且不能是根目录：%q", dir)
	}
	files, err := list_files()
	if err != nil {
		return result, err
	}
	result.Files = len(files)

	// 指纹一致且入口文件确实在，就认为上次导出是完整的
	marker := filepath.Join(dir, marker_name)
	if read_marker(marker) == result.Fingerprint && file_exists(filepath.Join(dir, "index.html")) {
		return result, nil
	}

	for _, name := range files {
		data, err := read_dist_file(name)
		if err != nil {
			return result, err
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return result, err
		}
		if err = os.WriteFile(target, data, 0o644); err != nil {
			return result, err
		}
	}
	if err = prune(dir, files); err != nil {
		return result, err
	}
	if err = os.WriteFile(marker, []byte(result.Fingerprint+"\n"), 0o644); err != nil {
		return result, err
	}
	result.Changed = true
	return result, nil
}

// list_files 列出内嵌资源里需要导出的文件，按路径排序保证指纹可复现
func list_files() ([]string, error) {
	sub, err := dist_fs()
	if err != nil {
		return nil, err
	}
	names := []string{}
	err = fs.WalkDir(sub, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if skip_names[filepath.Base(name)] || name == marker_name {
			return nil
		}
		names = append(names, name)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// prune 删掉导出目录里已经不属于当前版本的文件，并清掉随之空掉的目录。
// 这里刻意不做整体删除重建：导出目录由调用方给出，宁可留下一个空目录，
// 也不要因为路径写错而把别处的文件一起抹掉。
func prune(dir string, keep []string) error {
	expected := make(map[string]bool, len(keep)+1)
	for _, name := range keep {
		expected[filepath.FromSlash(name)] = true
	}
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			empty, err := dir_is_empty(path)
			if err == nil && empty && !expected[rel] {
				return os.Remove(path)
			}
			return nil
		}
		if expected[rel] || rel == marker_name {
			return nil
		}
		return os.Remove(path)
	})
}

// dir_is_empty 判断目录里是否已经没有任何条目
func dir_is_empty(path string) (bool, error) {
	handle, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer handle.Close()
	if _, err = handle.Readdirnames(1); err == io.EOF {
		return true, nil
	} else if err != nil {
		return false, err
	}
	return false, nil
}

// read_dist_file 读取内嵌资源里的一个文件
func read_dist_file(name string) ([]byte, error) {
	sub, err := dist_fs()
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(sub, strings.TrimPrefix(name, "/"))
}

// read_marker 读取指纹文件，读不到或格式不符时当作「没有指纹」
func read_marker(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// compute_fingerprint 把「路径 + 长度 + 内容」依次喂给 SHA-256，
// 路径与长度一起入哈希是为了让改名或移动文件也能被识别出来。
func compute_fingerprint() string {
	files, err := list_files()
	if err != nil || len(files) == 0 {
		return ""
	}
	digest := sha256.New()
	for _, name := range files {
		data, err := read_dist_file(name)
		if err != nil {
			return ""
		}
		fmt.Fprintf(digest, "%s\x00%d\x00", name, len(data))
		digest.Write(data)
	}
	return hex.EncodeToString(digest.Sum(nil))[:16]
}

// file_exists 判断路径存在且不是目录
func file_exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// dist_fs 返回内嵌资源子目录的视图，Handler 与导出器共用同一份实现
func dist_fs() (fs.FS, error) {
	return fs.Sub(dist_content, "dist")
}
