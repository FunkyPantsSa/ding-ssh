import {createApp} from 'vue'
import {createPinia} from 'pinia'
import App from './App.vue'
import {installDebugBridge} from './debug/bridge'
import './style.css'

const app = createApp(App)
app.use(createPinia())
app.mount('#app')

// 调试模式的前端桥：只有后端开启调试模式并下发 debug:request 时才会被触发。
installDebugBridge()
