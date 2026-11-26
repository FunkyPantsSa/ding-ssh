import {installI18n, syncBackendLanguage} from './i18n'
import {createApp} from 'vue'
import {createPinia} from 'pinia'
import App from './App.vue'
import {installDebugBridge} from './debug/bridge'
import {installDebugUI} from './debug/ui'
import {monitorFrontendRuntime} from './utils/runtime-status'
import {installKeyboardNavigation} from './utils/keyboard-navigation'
import 'virtual:ding-bundled-fonts.css'
import './style.css'

const app = createApp(App)
app.use(createPinia())
installI18n(app)
const stopKeyboardNavigation = installKeyboardNavigation()
if (import.meta.hot) import.meta.hot.dispose(stopKeyboardNavigation)
void syncBackendLanguage().catch(console.error).finally(() => app.mount('#app'))

// 调试模式的 UI 观测域：安装控制台 / window.onerror / Vue errorHandler 采集钩子
// （都是包裹而不是覆盖，应用原有行为不变），并记录本次页面的 revision。
// 未开启调试模式时它也只是几个内存里的钩子，没有任何网络或磁盘动作。
installDebugUI()

// 调试模式的前端桥：只有后端开启调试模式并下发 debug:request 时才会被触发。
installDebugBridge()

monitorFrontendRuntime()
