/// <reference types="vite/client" />

declare module '*.vue' {
    import type {DefineComponent} from 'vue'
    const component: DefineComponent<{}, {}, any>
    export default component
}

declare module 'zmodem.js/src/zmodem_browser.js' {
  const Zmodem: any
  export default Zmodem
}

declare module 'zmodem.js' {
  const Zmodem: any
  export default Zmodem
}

// 构建信息：由 vite.config.ts 的 define 在编译期注入。
declare const __APP_VERSION__: string
declare const __APP_COMMIT__: string
declare const __BUILD_TIME__: string
