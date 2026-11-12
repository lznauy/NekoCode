export namespace core {

	export class ImageAttachment {
	    label: string;
	    path: string;

	    static createFrom(source: any = {}) {
	        return new ImageAttachment(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.path = source["path"];
	    }
	}
	export class SenderRef {
	    id?: string;
	    username?: string;
	    display?: string;

	    static createFrom(source: any = {}) {
	        return new SenderRef(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.username = source["username"];
	        this.display = source["display"];
	    }
	}
	export class SourceRef {
	    kind: string;
	    id?: string;

	    static createFrom(source: any = {}) {
	        return new SourceRef(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.id = source["id"];
	    }
	}
	export class Input {
	    source: SourceRef;
	    sender: SenderRef;
	    text: string;
	    images?: ImageAttachment[];

	    static createFrom(source: any = {}) {
	        return new Input(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = this.convertValues(source["source"], SourceRef);
	        this.sender = this.convertValues(source["sender"], SenderRef);
	        this.text = source["text"];
	        this.images = this.convertValues(source["images"], ImageAttachment);
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

export namespace mode {

	export class Definition {
	    id: string;
	    version: number;
	    role: string;
	    strategy: string;
	    tool_policy: string;
	    label: string;
	    description: string;

	    static createFrom(source: any = {}) {
	        return new Definition(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.version = source["version"];
	        this.role = source["role"];
	        this.strategy = source["strategy"];
	        this.tool_policy = source["tool_policy"];
	        this.label = source["label"];
	        this.description = source["description"];
	    }
	}

}

export namespace protocol {

	export class CommandMenuItem {
	    key?: string;
	    value: string;
	    label: string;
	    description?: string;
	    submit?: boolean;
	    current?: boolean;

	    static createFrom(source: any = {}) {
	        return new CommandMenuItem(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.submit = source["submit"];
	        this.current = source["current"];
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
	export class ModelDefinition {
	    name?: string;
	    model: string;
	    protocol?: string;
	    reasoning_effort?: string;
	    context_window?: number;
	    vision?: boolean;

	    static createFrom(source: any = {}) {
	        return new ModelDefinition(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.model = source["model"];
	        this.protocol = source["protocol"];
	        this.reasoning_effort = source["reasoning_effort"];
	        this.context_window = source["context_window"];
	        this.vision = source["vision"];
	    }
	}
	export class RunSummary {
	    step_count: number;
	    stop_reason: string;

	    static createFrom(source: any = {}) {
	        return new RunSummary(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.step_count = source["step_count"];
	        this.stop_reason = source["stop_reason"];
	    }
	}

}

export namespace runtime {

	export class BackgroundDemand {
	    id: string;
	    session_id: string;
	    input: core.Input;
	    status: string;
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new BackgroundDemand(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.input = this.convertValues(source["input"], core.Input);
	        this.status = source["status"];
	        this.error = source["error"];
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
	export class BackgroundUsage {
	    total_tokens: number;
	    cache_hit_tokens: number;
	    cache_miss_tokens: number;
	    tool_use_count: number;
	    duration_ms: number;

	    static createFrom(source: any = {}) {
	        return new BackgroundUsage(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_tokens = source["total_tokens"];
	        this.cache_hit_tokens = source["cache_hit_tokens"];
	        this.cache_miss_tokens = source["cache_miss_tokens"];
	        this.tool_use_count = source["tool_use_count"];
	        this.duration_ms = source["duration_ms"];
	    }
	}
	export class BackgroundTask {
	    isolation?: string;
	    scheduling_note?: string;
	    queue_reason?: string;
	    result_truncated?: boolean;
	    after?: string[];
	    usage: BackgroundUsage;
	    attempt_usage: BackgroundUsage;
	    delivery_error?: string;
	    delivery_attempts?: number;
	    attempt_id?: string;
	    attempt_seq?: number;
	    prompt?: string;
	    update_sequence?: number;
	    pending_updates?: string[];
	    id: string;
	    session_id: string;
	    group_id: string;
	    title: string;
	    profile: string;
	    status: string;
	    result?: string;
	    error?: string;
	    notified: boolean;
	    delivery?: string;

	    static createFrom(source: any = {}) {
	        return new BackgroundTask(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isolation = source["isolation"];
	        this.scheduling_note = source["scheduling_note"];
	        this.queue_reason = source["queue_reason"];
	        this.result_truncated = source["result_truncated"];
	        this.after = source["after"];
	        this.usage = this.convertValues(source["usage"], BackgroundUsage);
	        this.attempt_usage = this.convertValues(source["attempt_usage"], BackgroundUsage);
	        this.delivery_error = source["delivery_error"];
	        this.delivery_attempts = source["delivery_attempts"];
	        this.attempt_id = source["attempt_id"];
	        this.attempt_seq = source["attempt_seq"];
	        this.prompt = source["prompt"];
	        this.update_sequence = source["update_sequence"];
	        this.pending_updates = source["pending_updates"];
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.group_id = source["group_id"];
	        this.title = source["title"];
	        this.profile = source["profile"];
	        this.status = source["status"];
	        this.result = source["result"];
	        this.error = source["error"];
	        this.notified = source["notified"];
	        this.delivery = source["delivery"];
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
	    keep_threshold?: number;
	    enabled?: boolean;

	    static createFrom(source: any = {}) {
	        return new JevConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.api_key = source["api_key"];
	        this.model = source["model"];
	        this.base_url = source["base_url"];
	        this.keep_threshold = source["keep_threshold"];
	        this.enabled = source["enabled"];
	    }
	}
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
	    headers?: Record<string, string>;
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
	        this.headers = source["headers"];
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
	export class ImageUnderstandConfig {
	    name: string;
	    provider: string;
	    api_key: string;
	    model: string;
	    base_url?: string;
	    protocol?: string;

	    static createFrom(source: any = {}) {
	        return new ImageUnderstandConfig(source);
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
	    vision?: boolean;
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
	        this.vision = source["vision"];
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
	export class ProviderConfig {
	    name: string;
	    base_url?: string;
	    api_key?: string;
	    protocol?: string;
	    models: protocol.ModelDefinition[];

	    static createFrom(source: any = {}) {
	        return new ProviderConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.base_url = source["base_url"];
	        this.api_key = source["api_key"];
	        this.protocol = source["protocol"];
	        this.models = this.convertValues(source["models"], protocol.ModelDefinition);
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
	export class ConfigView {
	    path: string;
	    exists: boolean;
	    active: string;
	    auto_compact_percent: number;
	    max_concurrent_subagents?: number;
	    compaction_model?: string;
	    providers: ProviderConfig[];
	    models: ModelConfig[];
	    image_gen_models?: ImageGenConfig[];
	    image_understand_models?: ImageUnderstandConfig[];
	    mcp_servers?: Record<string, MCPServerConfig>;
	    permissions?: PermissionsConfig;
	    workspaces?: WorkspaceConfig[];
	    jev?: JevConfig;

	    static createFrom(source: any = {}) {
	        return new ConfigView(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.exists = source["exists"];
	        this.active = source["active"];
	        this.auto_compact_percent = source["auto_compact_percent"];
	        this.max_concurrent_subagents = source["max_concurrent_subagents"];
	        this.compaction_model = source["compaction_model"];
	        this.providers = this.convertValues(source["providers"], ProviderConfig);
	        this.models = this.convertValues(source["models"], ModelConfig);
	        this.image_gen_models = this.convertValues(source["image_gen_models"], ImageGenConfig);
	        this.image_understand_models = this.convertValues(source["image_understand_models"], ImageUnderstandConfig);
	        this.mcp_servers = this.convertValues(source["mcp_servers"], MCPServerConfig, true);
	        this.permissions = this.convertValues(source["permissions"], PermissionsConfig);
	        this.workspaces = this.convertValues(source["workspaces"], WorkspaceConfig);
	        this.jev = this.convertValues(source["jev"], JevConfig);
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
	    sequence?: number;
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
	        this.sequence = source["sequence"];
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



	export class InteractionMode {
	    selected: string;
	    options: mode.Definition[];

	    static createFrom(source: any = {}) {
	        return new InteractionMode(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.selected = source["selected"];
	        this.options = this.convertValues(source["options"], mode.Definition);
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
	export class MemorySection {
	    key: string;
	    title: string;
	    content: string;
	    empty: boolean;

	    static createFrom(source: any = {}) {
	        return new MemorySection(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.empty = source["empty"];
	    }
	}
	export class MemoryView {
	    scope: string;
	    path?: string;
	    content: string;
	    sections?: MemorySection[];
	    empty: boolean;

	    static createFrom(source: any = {}) {
	        return new MemoryView(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.path = source["path"];
	        this.content = source["content"];
	        this.sections = this.convertValues(source["sections"], MemorySection);
	        this.empty = source["empty"];
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
	    activeAgents?: string[];
	    hookError?: string;
	    agentError?: string;

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
	        this.activeAgents = source["activeAgents"];
	        this.hookError = source["hookError"];
	        this.agentError = source["agentError"];
	    }
	}


	export class SessionMeta {
	    id: string;
	    cwd: string;
	    createdAt: number;
	    updatedAt: number;
	    msgCount: number;
	    forkedFrom?: string;
	    forkSourceDeleted?: boolean;

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
	        this.forkedFrom = source["forkedFrom"];
	        this.forkSourceDeleted = source["forkSourceDeleted"];
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

	export class SubagentAttemptSummary {
	    summary: protocol.RunSummary;
	    id: string;
	    seq: number;
	    prompt: string;
	    status: string;
	    result?: string;
	    // Go type: time
	    started_at: any;
	    // Go type: time
	    ended_at?: any;
	    transcript_start: number;
	    transcript_end: number;
	    total_tokens?: number;
	    cache_hit_tokens?: number;
	    cache_miss_tokens?: number;
	    tool_use_count?: number;
	    duration_ms?: number;
	    context_tokens?: number;
	    context_window?: number;

	    static createFrom(source: any = {}) {
	        return new SubagentAttemptSummary(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.summary = this.convertValues(source["summary"], protocol.RunSummary);
	        this.id = source["id"];
	        this.seq = source["seq"];
	        this.prompt = source["prompt"];
	        this.status = source["status"];
	        this.result = source["result"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.ended_at = this.convertValues(source["ended_at"], null);
	        this.transcript_start = source["transcript_start"];
	        this.transcript_end = source["transcript_end"];
	        this.total_tokens = source["total_tokens"];
	        this.cache_hit_tokens = source["cache_hit_tokens"];
	        this.cache_miss_tokens = source["cache_miss_tokens"];
	        this.tool_use_count = source["tool_use_count"];
	        this.duration_ms = source["duration_ms"];
	        this.context_tokens = source["context_tokens"];
	        this.context_window = source["context_window"];
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
	export class SubagentAttemptPage {
	    session_id: string;
	    subagent_id: string;
	    attempts: SubagentAttemptSummary[];
	    before: number;
	    end: number;
	    total: number;
	    has_more: boolean;

	    static createFrom(source: any = {}) {
	        return new SubagentAttemptPage(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.subagent_id = source["subagent_id"];
	        this.attempts = this.convertValues(source["attempts"], SubagentAttemptSummary);
	        this.before = source["before"];
	        this.end = source["end"];
	        this.total = source["total"];
	        this.has_more = source["has_more"];
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

	export class SubagentToolCall {
	    id: string;
	    name: string;
	    arguments: string;

	    static createFrom(source: any = {}) {
	        return new SubagentToolCall(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.arguments = source["arguments"];
	    }
	}
	export class SubagentRecord {
	    runtime_event?: boolean;
	    sequence?: number;
	    context_only?: boolean;
	    role: string;
	    content: string;
	    reasoning?: string;
	    tool_calls?: SubagentToolCall[];
	    tool_call_id?: string;
	    is_error?: boolean;
	    output_path?: string;
	    output_incomplete?: boolean;

	    static createFrom(source: any = {}) {
	        return new SubagentRecord(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runtime_event = source["runtime_event"];
	        this.sequence = source["sequence"];
	        this.context_only = source["context_only"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.reasoning = source["reasoning"];
	        this.tool_calls = this.convertValues(source["tool_calls"], SubagentToolCall);
	        this.tool_call_id = source["tool_call_id"];
	        this.is_error = source["is_error"];
	        this.output_path = source["output_path"];
	        this.output_incomplete = source["output_incomplete"];
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
	export class SubagentHistoryPage {
	    session_id: string;
	    subagent_id: string;
	    records: SubagentRecord[];
	    before: number;
	    end: number;
	    total: number;
	    has_more: boolean;

	    static createFrom(source: any = {}) {
	        return new SubagentHistoryPage(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.subagent_id = source["subagent_id"];
	        this.records = this.convertValues(source["records"], SubagentRecord);
	        this.before = source["before"];
	        this.end = source["end"];
	        this.total = source["total"];
	        this.has_more = source["has_more"];
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
	export class SubagentOutputPage {
	    content: string;
	    offset: number;
	    next_offset: number;
	    total: number;
	    has_more: boolean;
	    incomplete: boolean;

	    static createFrom(source: any = {}) {
	        return new SubagentOutputPage(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.offset = source["offset"];
	        this.next_offset = source["next_offset"];
	        this.total = source["total"];
	        this.has_more = source["has_more"];
	        this.incomplete = source["incomplete"];
	    }
	}
	export class SubagentSummary {
	    queue_reason?: string;
	    scheduling_note?: string;
	    workspace_kind?: string;
	    isolation?: string;
	    recorded: boolean;
	    title?: string;
	    id: string;
	    session_id: string;
	    profile: string;
	    skills?: string[];
	    prompt: string;
	    status: string;
	    phase?: string;
	    result?: string;
	    error?: string;
	    run_count: number;
	    transcript_seq: number;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    latest_attempt?: SubagentAttemptSummary;

	    static createFrom(source: any = {}) {
	        return new SubagentSummary(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.queue_reason = source["queue_reason"];
	        this.scheduling_note = source["scheduling_note"];
	        this.workspace_kind = source["workspace_kind"];
	        this.isolation = source["isolation"];
	        this.recorded = source["recorded"];
	        this.title = source["title"];
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.profile = source["profile"];
	        this.skills = source["skills"];
	        this.prompt = source["prompt"];
	        this.status = source["status"];
	        this.phase = source["phase"];
	        this.result = source["result"];
	        this.error = source["error"];
	        this.run_count = source["run_count"];
	        this.transcript_seq = source["transcript_seq"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.latest_attempt = this.convertValues(source["latest_attempt"], SubagentAttemptSummary);
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
	export class SubagentPage {
	    items: SubagentSummary[];
	    session_id: string;
	    total: number;
	    next_offset: number;
	    has_more: boolean;

	    static createFrom(source: any = {}) {
	        return new SubagentPage(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], SubagentSummary);
	        this.session_id = source["session_id"];
	        this.total = source["total"];
	        this.next_offset = source["next_offset"];
	        this.has_more = source["has_more"];
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



	export class TaskSearchHit {
	    sequence: number;
	    preview: string;

	    static createFrom(source: any = {}) {
	        return new TaskSearchHit(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sequence = source["sequence"];
	        this.preview = source["preview"];
	    }
	}
	export class TaskSearchPage {
	    hits: TaskSearchHit[];
	    next: number;
	    has_more: boolean;
	    total: number;

	    static createFrom(source: any = {}) {
	        return new TaskSearchPage(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hits = this.convertValues(source["hits"], TaskSearchHit);
	        this.next = source["next"];
	        this.has_more = source["has_more"];
	        this.total = source["total"];
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

