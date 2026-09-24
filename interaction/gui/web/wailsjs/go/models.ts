export namespace protocol {
	
	export class CommandMenuItem {
	    value: string;
	    label: string;
	    description?: string;
	    submit?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CommandMenuItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.submit = source["submit"];
	    }
	}
	export class CommandMenu {
	    title: string;
	    empty?: string;
	    items: CommandMenuItem[];
	
	    static createFrom(source: any = {}) {
	        return new CommandMenu(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.empty = source["empty"];
	        this.items = this.convertValues(source["items"], CommandMenuItem);
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

export namespace runtime {
	
	export class WorkspaceConfig {
	    path: string;
	    access?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.access = source["access"];
	    }
	}
	export class SandboxConfig {
	    sandbox_mode?: string;
	    network?: boolean;
	    writable_roots?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SandboxConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sandbox_mode = source["sandbox_mode"];
	        this.network = source["network"];
	        this.writable_roots = source["writable_roots"];
	    }
	}
	export class PermissionsConfig {
	    allow?: string[];
	    ask?: string[];
	    deny?: string[];
	    sandbox?: Record<string, SandboxConfig>;
	
	    static createFrom(source: any = {}) {
	        return new PermissionsConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.allow = source["allow"];
	        this.ask = source["ask"];
	        this.deny = source["deny"];
	        this.sandbox = this.convertValues(source["sandbox"], SandboxConfig, true);
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
	export class MCPServerConfig {
	    url?: string;
	    oauth_client_id?: string;
	    oauth_client_secret?: string;
	    oauth_client_metadata_url?: string;
	    oauth_callback_port?: number;
	    command: string;
	    args?: string[];
	    env?: Record<string, string>;
	    enabled: boolean;

	    static createFrom(source: any = {}) {
	        return new MCPServerConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.oauth_client_id = source["oauth_client_id"];
	        this.oauth_client_secret = source["oauth_client_secret"];
	        this.oauth_client_metadata_url = source["oauth_client_metadata_url"];
	        this.oauth_callback_port = source["oauth_callback_port"];

	        this.command = source["command"];
	        this.args = source["args"];
	        this.env = source["env"];
	        this.enabled = source["enabled"];
	    }
	}
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
	export class ModelProfile {
	    context_window: number;
	    context_window_source: string;
	    reasoning_efforts?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ModelProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.context_window = source["context_window"];
	        this.context_window_source = source["context_window_source"];
	        this.reasoning_efforts = source["reasoning_efforts"];
	    }
	}
	export class ModelConfig {
	    name: string;
	    provider: string;
	    api_key: string;
	    model: string;
	    base_url?: string;
	    protocol?: string;
	    reasoning_effort?: string;
	    context_window?: number;
	    profile: ModelProfile;
	
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
	        this.reasoning_effort = source["reasoning_effort"];
	        this.context_window = source["context_window"];
	        this.profile = this.convertValues(source["profile"], ModelProfile);
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
	export class JevConfig {
	    api_key?: string;
	    model?: string;
	    base_url?: string;
	    keep_threshold?: number | null;
	    enabled?: boolean | null;

	    static createFrom(source: any = {}) { return new JevConfig(source); }
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.api_key = source["api_key"];
	        this.model = source["model"];
	        this.base_url = source["base_url"];
	        this.keep_threshold = source["keep_threshold"];
	        this.enabled = source["enabled"];
	    }
	}

	export class ConfigView {
	    jev?: JevConfig;
	    path: string;
	    exists: boolean;
	    active: string;
	    auto_compact_percent: number;
	    flash_model?: string;
	    models: ModelConfig[];
	    image_gen_models?: ImageGenConfig[];
	    mcp_servers?: Record<string, MCPServerConfig>;
	    permissions?: PermissionsConfig;
	    workspaces?: WorkspaceConfig[];
	
	    static createFrom(source: any = {}) {
	        return new ConfigView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.jev = this.convertValues(source["jev"], JevConfig);
	        this.path = source["path"];
	        this.exists = source["exists"];
	        this.active = source["active"];
	        this.auto_compact_percent = source["auto_compact_percent"];
	        this.flash_model = source["flash_model"];
	        this.models = this.convertValues(source["models"], ModelConfig);
	        this.image_gen_models = this.convertValues(source["image_gen_models"], ImageGenConfig);
	        this.mcp_servers = this.convertValues(source["mcp_servers"], MCPServerConfig, true);
	        this.permissions = this.convertValues(source["permissions"], PermissionsConfig);
	        this.workspaces = this.convertValues(source["workspaces"], WorkspaceConfig);
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
	    compactionThreshold: number;
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
	        this.compactionThreshold = source["compactionThreshold"];
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
	    toolName: string;
	    args?: string;
	    content: string;
	    isError?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DisplayBlock(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.toolName = source["toolName"];
	        this.args = source["args"];
	        this.content = source["content"];
	        this.isError = source["isError"];
	    }
	}
	export class ImageRef {
	    path: string;
	    url?: string;
	    width?: number;
	    height?: number;
	
	    static createFrom(source: any = {}) {
	        return new ImageRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.url = source["url"];
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}
	export class DisplayMessage {
	    role: string;
	    content: string;
	    reasoning?: string;
	    blocks?: DisplayBlock[];
	    images?: ImageRef[];
	
	    static createFrom(source: any = {}) {
	        return new DisplayMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	        this.reasoning = source["reasoning"];
	        this.blocks = this.convertValues(source["blocks"], DisplayBlock);
	        this.images = this.convertValues(source["images"], ImageRef);
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
	    url?: string;
	    authUrl?: string;
	    name: string;
	    plugin: string;
	    command: string;
	    args?: string[];
	    pluginEnabled: boolean;
	    status?: string;
	    error?: string;
	    toolCount?: number;
	
	    static createFrom(source: any = {}) {
	        return new MCPServerView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.authUrl = source["authUrl"];

	        this.name = source["name"];
	        this.plugin = source["plugin"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.pluginEnabled = source["pluginEnabled"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.toolCount = source["toolCount"];
	    }
	}
	
	
	export class ModelSpec {
	    provider: string;
	    model: string;
	    protocol?: string;
	    context_window?: number;
	
	    static createFrom(source: any = {}) {
	        return new ModelSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.protocol = source["protocol"];
	        this.context_window = source["context_window"];
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
	
	export class SessionMeta {
	    id: string;
	    cwd: string;
	    createdAt: number;
	    updatedAt: number;
	    msgCount: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.cwd = source["cwd"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.msgCount = source["msgCount"];
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

