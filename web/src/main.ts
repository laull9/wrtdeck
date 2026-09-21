import { createApp } from 'vue'
import './style.css'
import App from './App.vue'
import { bootstrap } from './lib/auth'

// 先把登录状态定下来再挂载界面：
// 首帧渲染出来的就是已登录状态，不会先闪一下登录页，也不会先报一次 401。
bootstrap().finally(() => {
  createApp(App).mount('#app')
})
