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
	}): Promise<{ settings: Settings }> => request('PATCH', '/api/settings', patch),

	testTelegram: (): Promise<{
		ok: boolean;
		bot: { id: number; username: string; first_name: string };
	}> => request('POST', '/api/settings/test-telegram', {}),

	testAI: (): Promise<{ ok: boolean; model: string; latency_ms: number }> =>
		request('POST', '/api/settings/test-ai', {}),

	// Health
	getHealth: (): Promise<Health> => request('GET', '/api/health')
};
