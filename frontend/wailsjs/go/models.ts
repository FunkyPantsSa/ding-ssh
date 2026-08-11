export namespace models {
	
	export class ConnectResult {
	    sessionId: string;
	    server: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.server = source["server"];
	    }
	}
	export class Credential {
	    id: string;
	    name: string;
	    user: string;
	    password?: string;
	    authType: string;
	    keyPath?: string;
	    keyContent?: string;
	
	    static createFrom(source: any = {}) {
	        return new Credential(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.user = source["user"];
	        this.password = source["password"];
	        this.authType = source["authType"];
	        this.keyPath = source["keyPath"];
	        this.keyContent = source["keyContent"];
	    }
	}
	export class SFTPEntry {
	    name: string;
	    path: string;
	    isDir: boolean;
	    size: number;
	    modTime: number;
	
	    static createFrom(source: any = {}) {
	        return new SFTPEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.size = source["size"];
	        this.modTime = source["modTime"];
	    }
	}
	export class ServerNode {
	    id: string;
	    name: string;
	    group: string;
	    host: string;
	    port: number;
	    user: string;
	    authType: string;
	    password?: string;
	    keyPath?: string;
	    keyContent?: string;
	    bgImage: string;
	    blurAmount: number;
	    envVars: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ServerNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.group = source["group"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.authType = source["authType"];
	        this.password = source["password"];
	        this.keyPath = source["keyPath"];
	        this.keyContent = source["keyContent"];
	        this.bgImage = source["bgImage"];
	        this.blurAmount = source["blurAmount"];
	        this.envVars = source["envVars"];
	    }
	}
	export class SessionInfo {
	    sessionId: string;
	    serverName: string;
	    host: string;
	    user: string;
	    status: string;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.serverName = source["serverName"];
	        this.host = source["host"];
	        this.user = source["user"];
	        this.status = source["status"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class Theme {
	    background: string;
	    foreground: string;
	    cursor: string;
	    selection: string;
	    bgImage: string;
	    blurAmount: number;
	    textShadow: boolean;
	    shadowBlur: number;
	
	    static createFrom(source: any = {}) {
	        return new Theme(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.background = source["background"];
	        this.foreground = source["foreground"];
	        this.cursor = source["cursor"];
	        this.selection = source["selection"];
	        this.bgImage = source["bgImage"];
	        this.blurAmount = source["blurAmount"];
	        this.textShadow = source["textShadow"];
	        this.shadowBlur = source["shadowBlur"];
	    }
	}
	export class Settings {
	    logEnabled: boolean;
	    copyOnSelect: boolean;
	    theme: Theme;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.logEnabled = source["logEnabled"];
	        this.copyOnSelect = source["copyOnSelect"];
	        this.theme = this.convertValues(source["theme"], Theme);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class TunnelInfo {
	    id: string;
	    name: string;
	    serverId: string;
	    serverName: string;
	    localPort: number;
	    remoteHost: string;
	    remotePort: number;
	    status: string;
	    message?: string;
	    startedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new TunnelInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.serverId = source["serverId"];
	        this.serverName = source["serverName"];
	        this.localPort = source["localPort"];
	        this.remoteHost = source["remoteHost"];
	        this.remotePort = source["remotePort"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.startedAt = source["startedAt"];
	    }
	}

}

