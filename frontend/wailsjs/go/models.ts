export namespace main {

	export class AnnouncementSettings {
	    id: string;
	    enabled: boolean;
	    kind: string;
	    command: string;
	    afterMinutes: number;
	    repeatMinutes: number;
	    message: string;

	    static createFrom(source: any = {}) {
	        return new AnnouncementSettings(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.enabled = source["enabled"];
	        this.kind = source["kind"];
	        this.command = source["command"];
	        this.afterMinutes = source["afterMinutes"];
	        this.repeatMinutes = source["repeatMinutes"];
	        this.message = source["message"];
	    }
	}
	export class ControlSettings {
	    running: boolean;
	    status: string;
	    error: string;
	    channel: string;
	    botUsername: string;
	    configPath: string;
	    twitchOAuthToken: string;
	    twitchRefreshToken: string;
	    twitchClientId: string;
	    twitchClientSecret: string;
	    twitchAdsOAuthToken: string;
	    twitchAdsRefreshToken: string;
	    hasTwitchOAuthToken: boolean;
	    hasTwitchRefreshToken: boolean;
	    hasTwitchClientId: boolean;
	    hasTwitchClientSecret: boolean;
	    hasTwitchAdsOAuthToken: boolean;
	    hasTwitchAdsRefreshToken: boolean;
	    aiProvider: string;
	    aiApiKey: string;
	    geminiApiKey: string;
	    aiModel: string;
	    geminiModel: string;
	    maxRequestsPerHour: number;
	    dailyBudgetUsd: number;
	    monthlyBudgetUsd: number;
	    hasAiApiKey: boolean;
	    hasGeminiApiKey: boolean;
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
	    announcementsEnabled: boolean;
	    announcementPollSeconds: number;
	
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
	        this.configPath = source["configPath"];
	        this.twitchOAuthToken = source["twitchOAuthToken"];
	        this.twitchRefreshToken = source["twitchRefreshToken"];
	        this.twitchClientId = source["twitchClientId"];
	        this.twitchClientSecret = source["twitchClientSecret"];
	        this.twitchAdsOAuthToken = source["twitchAdsOAuthToken"];
	        this.twitchAdsRefreshToken = source["twitchAdsRefreshToken"];
	        this.hasTwitchOAuthToken = source["hasTwitchOAuthToken"];
	        this.hasTwitchRefreshToken = source["hasTwitchRefreshToken"];
	        this.hasTwitchClientId = source["hasTwitchClientId"];
	        this.hasTwitchClientSecret = source["hasTwitchClientSecret"];
	        this.hasTwitchAdsOAuthToken = source["hasTwitchAdsOAuthToken"];
	        this.hasTwitchAdsRefreshToken = source["hasTwitchAdsRefreshToken"];
	        this.aiProvider = source["aiProvider"];
	        this.aiApiKey = source["aiApiKey"];
	        this.geminiApiKey = source["geminiApiKey"];
	        this.aiModel = source["aiModel"];
	        this.geminiModel = source["geminiModel"];
	        this.maxRequestsPerHour = source["maxRequestsPerHour"];
	        this.dailyBudgetUsd = source["dailyBudgetUsd"];
	        this.monthlyBudgetUsd = source["monthlyBudgetUsd"];
	        this.hasAiApiKey = source["hasAiApiKey"];
	        this.hasGeminiApiKey = source["hasGeminiApiKey"];
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
	        this.announcementsEnabled = source["announcementsEnabled"];
	        this.announcementPollSeconds = source["announcementPollSeconds"];
	    }
	}

}
