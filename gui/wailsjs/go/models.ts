export namespace common {
	
	export class DisplayBlock {
	    ToolName: string;
	    Args: string;
	    Content: string;
	
	    static createFrom(source: any = {}) {
	        return new DisplayBlock(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ToolName = source["ToolName"];
	        this.Args = source["Args"];
	        this.Content = source["Content"];
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

