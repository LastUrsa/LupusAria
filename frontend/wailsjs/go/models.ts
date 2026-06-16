export namespace main {
	
	export class ControlSettings {
	    running: boolean;
	    status: string;
	    error: string;
	    channel: string;
	    botUsername: string;
	    aiProvider: string;
	    aiModel: string;
	    geminiModel: string;
	    maxRequestsPerHour: number;
	    dailyBudgetUsd: number;
	    monthlyBudgetUsd: number;
	    enableMentions: boolean;
	    enableAsk: boolean;
	    enableLurk: boolean;
	    enableCommands: boolean;
	    enableReset: boolean;
	    globalCooldownSeconds: number;
	    userCooldownSeconds: number;
	    maxContextMessages: number;
	    autosoEnabled: boolean;
	    recentStreamerMinWatch: number;
	    recentStreamerDays: number;
	    recentStreamerPageSize: number;
	    recentStreamerDelay: number;
	    adAlertsEnabled: boolean;
	    adWarningMinutes: number;
	    adPollSeconds: number;
	    adWarningMessage: string;
	    adStartMessage: string;
	    adEndMessage: string;
	
	    static createFrom(source: any = {}) {
	        return new ControlSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.channel = source["channel"];
	        this.botUsername = source["botUsername"];
	        this.aiProvider = source["aiProvider"];
	        this.aiModel = source["aiModel"];
	        this.geminiModel = source["geminiModel"];
	        this.maxRequestsPerHour = source["maxRequestsPerHour"];
	        this.dailyBudgetUsd = source["dailyBudgetUsd"];
	        this.monthlyBudgetUsd = source["monthlyBudgetUsd"];
	        this.enableMentions = source["enableMentions"];
	        this.enableAsk = source["enableAsk"];
	        this.enableLurk = source["enableLurk"];
	        this.enableCommands = source["enableCommands"];
	        this.enableReset = source["enableReset"];
	        this.globalCooldownSeconds = source["globalCooldownSeconds"];
	        this.userCooldownSeconds = source["userCooldownSeconds"];
	        this.maxContextMessages = source["maxContextMessages"];
	        this.autosoEnabled = source["autosoEnabled"];
	        this.recentStreamerMinWatch = source["recentStreamerMinWatch"];
	        this.recentStreamerDays = source["recentStreamerDays"];
	        this.recentStreamerPageSize = source["recentStreamerPageSize"];
	        this.recentStreamerDelay = source["recentStreamerDelay"];
	        this.adAlertsEnabled = source["adAlertsEnabled"];
	        this.adWarningMinutes = source["adWarningMinutes"];
	        this.adPollSeconds = source["adPollSeconds"];
	        this.adWarningMessage = source["adWarningMessage"];
	        this.adStartMessage = source["adStartMessage"];
	        this.adEndMessage = source["adEndMessage"];
	    }
	}

}

