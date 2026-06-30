export namespace common {
	
	export class ContextSegment {
	    key: string;
	    label: string;
	    tokens: number;
	    tone: string;
	
	    static createFrom(source: any = {}) {
	        return new ContextSegment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.tokens = source["tokens"];
	        this.tone = source["tone"];
	    }
	}
	export class ContextSnapshot {
	    budget: number;
	    used: number;
	    free: number;
	    percentUsed: number;
	    systemPrompt: number;
	    toolDefTokens: number;
	    todoText: number;
	    skillList: number;
	    messageTokens: number;
	    toolDefCount: number;
	    messageCount: number;
	    userMessages: number;
	    assistantMsgs: number;
	    toolResults: number;
	    archived: number;
	    compactCount: number;
	    trimCount: number;
	    cacheHitTokens: number;
	    cacheMissTokens: number;
	    cacheHitRatio: number;
	    subCount: number;
	    subTokens: number;
	    subCacheHit: number;
	    subCacheMiss: number;
	    segments: ContextSegment[];
	
	    static createFrom(source: any = {}) {
	        return new ContextSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.budget = source["budget"];
	        this.used = source["used"];
	        this.free = source["free"];
	        this.percentUsed = source["percentUsed"];
	        this.systemPrompt = source["systemPrompt"];
	        this.toolDefTokens = source["toolDefTokens"];
	        this.todoText = source["todoText"];
	        this.skillList = source["skillList"];
	        this.messageTokens = source["messageTokens"];
	        this.toolDefCount = source["toolDefCount"];
	        this.messageCount = source["messageCount"];
	        this.userMessages = source["userMessages"];
	        this.assistantMsgs = source["assistantMsgs"];
	        this.toolResults = source["toolResults"];
	        this.archived = source["archived"];
	        this.compactCount = source["compactCount"];
	        this.trimCount = source["trimCount"];
	        this.cacheHitTokens = source["cacheHitTokens"];
	        this.cacheMissTokens = source["cacheMissTokens"];
	        this.cacheHitRatio = source["cacheHitRatio"];
	        this.subCount = source["subCount"];
	        this.subTokens = source["subTokens"];
	        this.subCacheHit = source["subCacheHit"];
	        this.subCacheMiss = source["subCacheMiss"];
	        this.segments = this.convertValues(source["segments"], ContextSegment);
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
	
	export class MCPServerView {
	    name: string;
	    plugin: string;
	    command: string;
	    args?: string[];
	    dangerLevel?: string;
	    pluginEnabled: boolean;
	    status?: string;
	    error?: string;
	    toolCount?: number;
	
	    static createFrom(source: any = {}) {
	        return new MCPServerView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.plugin = source["plugin"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.dangerLevel = source["dangerLevel"];
	        this.pluginEnabled = source["pluginEnabled"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.toolCount = source["toolCount"];
	    }
	}
	export class PluginView {
	    name: string;
	    version?: string;
	    description?: string;
	    source?: string;
	    dir?: string;
	    enabled: boolean;
	    skills?: string[];
	    skillNames?: string[];
	    agents?: string[];
	    commands?: string[];
	    mcpServers?: string[];
	    hasHooks?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PluginView(source);
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
	        this.skillNames = source["skillNames"];
	        this.agents = source["agents"];
	        this.commands = source["commands"];
	        this.mcpServers = source["mcpServers"];
	        this.hasHooks = source["hasHooks"];
	    }
	}
	export class SkillView {
	    name: string;
	    description?: string;
	    dir?: string;
	    files?: string[];
	    loaded: boolean;
	    source: string;
	    sourceKind: string;
	    plugin?: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.dir = source["dir"];
	        this.files = source["files"];
	        this.loaded = source["loaded"];
	        this.source = source["source"];
	        this.sourceKind = source["sourceKind"];
	        this.plugin = source["plugin"];
	    }
	}
	export class SkillManagementView {
	    skills: SkillView[];
	    plugins: PluginView[];
	    mcp: MCPServerView[];
	
	    static createFrom(source: any = {}) {
	        return new SkillManagementView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skills = this.convertValues(source["skills"], SkillView);
	        this.plugins = this.convertValues(source["plugins"], PluginView);
	        this.mcp = this.convertValues(source["mcp"], MCPServerView);
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
	export class MCPServerConfig {
	    command: string;
	    args?: string[];
	    env?: Record<string, string>;
	    dangerLevel?: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MCPServerConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.command = source["command"];
	        this.args = source["args"];
	        this.env = source["env"];
	        this.dangerLevel = source["dangerLevel"];
	        this.enabled = source["enabled"];
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
	export class View {
	    path: string;
	    exists: boolean;
	    active: string;
	    context_window: number;
	    flash_model?: string;
	    models: ModelConfig[];
	    image_gen_models?: ImageGenConfig[];
	    mcp_servers?: Record<string, MCPServerConfig>;
	
	    static createFrom(source: any = {}) {
	        return new View(source);
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
	        this.mcp_servers = this.convertValues(source["mcp_servers"], MCPServerConfig, true);
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

