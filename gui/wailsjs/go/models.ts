export namespace common {
	
	export class DisplayBlock {
	    ToolName: string;
	    Args: string;
	    Content: string;
	    IsError: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DisplayBlock(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ToolName = source["ToolName"];
	        this.Args = source["Args"];
	        this.Content = source["Content"];
	        this.IsError = source["IsError"];
	    }
	}
	export class ImageRef {
	    Path: string;
	    URL: string;
	    Width: number;
	    Height: number;
	
	    static createFrom(source: any = {}) {
	        return new ImageRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.URL = source["URL"];
	        this.Width = source["Width"];
	        this.Height = source["Height"];
	    }
	}
	export class DisplayMessage {
	    Role: string;
	    Content: string;
	    Blocks: DisplayBlock[];
	    Images: ImageRef[];
	
	    static createFrom(source: any = {}) {
	        return new DisplayMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Role = source["Role"];
	        this.Content = source["Content"];
	        this.Blocks = this.convertValues(source["Blocks"], DisplayBlock);
	        this.Images = this.convertValues(source["Images"], ImageRef);
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

}

export namespace config {
	
	export class ImageGenConfig {
	    name: string;
	    provider: string;
	    api_key: string;
	    secret_key: string;
	    base_url?: string;
	    model?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImageGenConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.provider = source["provider"];
	        this.api_key = source["api_key"];
	        this.secret_key = source["secret_key"];
	        this.base_url = source["base_url"];
	        this.model = source["model"];
	    }
	}
	export class ModelConfig {
	    name: string;
	    provider: string;
	    api_key: string;
	    model: string;
	    base_url?: string;
	    protocol?: string;
	
	    static createFrom(source: any = {}) {
	        return new ModelConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.provider = source["provider"];
	        this.api_key = source["api_key"];
	        this.model = source["model"];
	        this.base_url = source["base_url"];
	        this.protocol = source["protocol"];
	    }
	}
	export class Snapshot {
	    path: string;
	    exists: boolean;
	    active: string;
	    context_window: number;
	    flash_model?: string;
	    models: ModelConfig[];
	    image_gen_models?: ImageGenConfig[];
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.exists = source["exists"];
	        this.active = source["active"];
	        this.context_window = source["context_window"];
	        this.flash_model = source["flash_model"];
	        this.models = this.convertValues(source["models"], ModelConfig);
	        this.image_gen_models = this.convertValues(source["image_gen_models"], ImageGenConfig);
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

}

export namespace plugin {
	
	export class Snapshot {
	    name: string;
	    version?: string;
	    description?: string;
	    source?: string;
	    dir?: string;
	    enabled: boolean;
	    skills?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.description = source["description"];
	        this.source = source["source"];
	        this.dir = source["dir"];
	        this.enabled = source["enabled"];
	        this.skills = source["skills"];
	    }
	}

}

export namespace session {
	
	export class Meta {
	    id: string;
	    cwd: string;
	    created_at: number;
	    updated_at: number;
	    msg_count: number;
	
	    static createFrom(source: any = {}) {
	        return new Meta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.cwd = source["cwd"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.msg_count = source["msg_count"];
	    }
	}

}

export namespace skill {
	
	export class Snapshot {
	    name: string;
	    description?: string;
	    dir?: string;
	    files?: string[];
	    loaded: boolean;
	    source: string;
	    plugin?: string;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.dir = source["dir"];
	        this.files = source["files"];
	        this.loaded = source["loaded"];
	        this.source = source["source"];
	        this.plugin = source["plugin"];
	    }
	}
	export class ManagementSnapshot {
	    skills: Snapshot[];
	    plugins: plugin.Snapshot[];
	
	    static createFrom(source: any = {}) {
	        return new ManagementSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skills = this.convertValues(source["skills"], Snapshot);
	        this.plugins = this.convertValues(source["plugins"], plugin.Snapshot);
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

}

