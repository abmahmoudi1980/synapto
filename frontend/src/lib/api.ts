// Typed API client for the Synapto admin backend.
// All methods return the parsed JSON response or throw on non-2xx.

export interface Channel {
	id: string;
	handle: string;
	display_name: string;
	status: 'active' | 'inaccessible' | 'banned';
	last_observed_at: string | null;
	last_error: string | null;
}

export interface Category {
	id: string;
	name: string;
	ordering: number;
	is_default: boolean;
}

export interface Settings {
	digest_interval_seconds: number;
	telegram_bot_token_ref: string;
	telegram_subscriber_chat: number;
	telegram_bot_reachable: boolean | null;
	ai_provider: string;
	ai_model: string;
	ai_base_url: string;
	ai_api_key_ref: string;
	ai_reachable: boolean | null;
	uncategorized_label: string;
	delivery_mode: 'bundled' | 'per_post';
	updated_at: string;
}

export interface Health {
	status: string;
	version: string;
	uptime_seconds: number;
	last_successful_cycle_at: string | null;
	last_failure_at: string | null;
	last_failure_reason: string | null;
	scheduler_state: string;
	db_ok: boolean;
}

export interface CycleListEntry {
	id: string;
	window_start: string;
	window_end: string;
	status: 'pending' | 'succeeded' | 'failed' | 'degraded' | 'skipped_no_items';
	input_msg_count: number;
	output_items: number;
	degraded: boolean;
	started_at: string;
	finished_at: string;
}

export interface DigestItemChannel {
	id: string;
	handle: string;
	display_name: string;
}

export interface DigestItem {
	id: string;
	channel: DigestItemChannel;
	source_msg_id: number;
	media_kind: string;
	summary: string;
	confidence: number | null;
}

export interface CategorySummary {
	id: string;
	name: string;
	ordering: number;
	is_default: boolean;
}

export interface ItemsByCategory {
	category: CategorySummary;
	items: DigestItem[];
}

export interface DigestInfo {
	id: string;
	rendered_text: string;
	degraded: boolean;
	telegram_msg_id: number | null;
	sent_at: string;
	send_status: 'ok' | 'failed' | 'blocked';
}

export interface CycleDetail {
	cycle: CycleListEntry;
	digest: DigestInfo;
	items_by_category: ItemsByCategory[];
}

export interface OpEvent {
	id: number;
	occurred_at: string;
	level: 'info' | 'warn' | 'error';
	kind: string;
	cycle_id?: string;
	message: string;
	context?: string;
}

export interface AuthStatus {
	authenticated: boolean;
	auth_required: boolean;
}

export interface ApiError {
	error: {
		code: string;
		message: string;
		field?: string;
	};
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
	const opts: RequestInit = { method, headers: {} };
	if (body !== undefined) {
		opts.headers = { 'Content-Type': 'application/json' };
		opts.body = JSON.stringify(body);
	}
	const res = await fetch(path, opts);
	const text = await res.text();
	if (!res.ok) {
		let err: ApiError;
		try {
			err = JSON.parse(text);
		} catch {
			throw new Error(`HTTP ${res.status}: ${text}`);
		}
		throw new Error(err.error?.message || `HTTP ${res.status}`);
	}
	if (res.status === 204 || text === '') {
		return undefined as T;
	}
	return JSON.parse(text) as T;
}

export const api = {
	// Channels
	listChannels: (): Promise<{ channels: Channel[] }> => request('GET', '/api/channels'),

	addChannel: (handle: string): Promise<{ channel: Channel }> =>
		request('POST', '/api/channels', { handle }),

	removeChannel: (id: string): Promise<void> => request('DELETE', `/api/channels/${id}`),

	// Categories
	listCategories: (): Promise<{ categories: Category[] }> => request('GET', '/api/categories'),

	addCategory: (name: string): Promise<{ category: Category }> =>
		request('POST', '/api/categories', { name }),

	renameCategory: (id: string, name: string): Promise<{ category: Category }> =>
		request('PATCH', `/api/categories/${id}`, { name }),

	removeCategory: (id: string): Promise<void> => request('DELETE', `/api/categories/${id}`),

	// Settings
	getSettings: (): Promise<{ settings: Settings }> => request('GET', '/api/settings'),

	patchSettings: (patch: {
		digest_interval_seconds?: number;
		telegram_subscriber_chat?: number;
		uncategorized_label?: string;
		delivery_mode?: 'bundled' | 'per_post';
	}): Promise<{ settings: Settings }> => request('PATCH', '/api/settings', patch),

	testTelegram: (): Promise<{
		ok: boolean;
		bot: { id: number; username: string; first_name: string };
	}> => request('POST', '/api/settings/test-telegram', {}),

	testAI: (): Promise<{ ok: boolean; model: string; latency_ms: number }> =>
		request('POST', '/api/settings/test-ai', {}),

	// Health
	getHealth: (): Promise<Health> => request('GET', '/api/health'),

	// Auth
	getAuthStatus: (): Promise<AuthStatus> => request('GET', '/api/auth/status'),

	login: (password: string): Promise<{ authenticated: boolean; expires_at?: string }> =>
		request('POST', '/api/auth/login', { password }),

	logout: (): Promise<{ authenticated: false }> => request('POST', '/api/auth/logout', {}),

	// History
	listCycles: (params?: {
		limit?: number;
		offset?: number;
	}): Promise<{ cycles: CycleListEntry[]; total: number }> => {
		const q = new URLSearchParams();
		if (params?.limit !== undefined) q.set('limit', String(params.limit));
		if (params?.offset !== undefined) q.set('offset', String(params.offset));
		const qs = q.toString();
		return request('GET', `/api/cycles${qs ? '?' + qs : ''}`);
	},

	getCycle: (id: string): Promise<CycleDetail> => request('GET', `/api/cycles/${id}`),

	listEvents: (params?: { limit?: number }): Promise<{ events: OpEvent[] }> => {
		const q = new URLSearchParams();
		if (params?.limit !== undefined) q.set('limit', String(params.limit));
		const qs = q.toString();
		return request('GET', `/api/events${qs ? '?' + qs : ''}`);
	},

	// Posts (per-post delivery queue)
	listPosts: (params?: {
		status?:
			| 'received'
			| 'summarized'
			| 'included_in_digest'
			| 'sent'
			| 'send_failed'
			| 'filtered_out'
			| 'dead';
		limit?: number;
	}): Promise<{ posts: Post[]; count: number }> => {
		const q = new URLSearchParams();
		if (params?.status !== undefined) q.set('status', params.status);
		if (params?.limit !== undefined) q.set('limit', String(params.limit));
		const qs = q.toString();
		return request('GET', `/api/posts${qs ? '?' + qs : ''}`);
	},

	getPost: (id: string): Promise<{ post: Post }> => request('GET', `/api/posts/${id}`)
};

export interface Post {
	id: string;
	channel_id: string;
	channel_handle?: string;
	source_msg_id: number;
	link: string;
	raw_text: string;
	media_kind: string;
	captured_at: string;
	status:
		| 'received'
		| 'summarized'
		| 'included_in_digest'
		| 'sent'
		| 'send_failed'
		| 'filtered_out'
		| 'dead';
	category_id?: string;
	category_name?: string;
	summary: string;
	confidence?: number;
	attempts: number;
	last_attempt_at?: string;
	sent_at?: string;
	telegram_msg_id?: number;
	send_error?: string;
}
