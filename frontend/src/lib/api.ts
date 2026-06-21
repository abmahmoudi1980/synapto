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

	// Health
	getHealth: (): Promise<Health> => request('GET', '/api/health')
};
