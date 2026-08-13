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
