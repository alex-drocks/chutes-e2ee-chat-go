export namespace main {
	
	export class chatPayload {
	    requestId: string;
	    params: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new chatPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.params = source["params"];
	    }
	}

}

