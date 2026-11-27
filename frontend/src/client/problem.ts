import type {VikunjaErrorModel} from '@/client/generated'

// A thrown plain object reaches Sentry without a title or stack, so every failed response becomes one of these.
export class ApiError extends Error implements VikunjaErrorModel {
	declare readonly status: number
	declare readonly detail: string
	declare readonly code?: number
	declare readonly details?: unknown
	declare readonly title?: string
	declare readonly errors?: VikunjaErrorModel['errors']
	declare readonly i18n_params?: VikunjaErrorModel['i18n_params']
	declare readonly type?: string
	declare readonly instance?: string

	constructor(problem: VikunjaErrorModel & {status: number, detail: string}) {
		super(problem.detail)
		Object.assign(this, problem)
		this.name = 'ApiError'
	}
}

function isWireProblem(error: unknown): error is Record<string, unknown> {
	return typeof error === 'object' && error !== null && Object.getPrototypeOf(error) === Object.prototype
}

function nonEmptyString(value: unknown): string | undefined {
	return typeof value === 'string' && value.trim() !== '' ? value.trim() : undefined
}

// A proxy's error page (e.g. Cloudflare's 413) is HTML, which is no use as a message.
function plainText(value: unknown): string | undefined {
	const text = nonEmptyString(value)
	return text !== undefined && /^<(!doctype|html|head|body)\b/i.test(text) ? undefined : text
}

// Echo rejects some /api/v2 requests before Huma (rate limits, invalid JWT) with a v1-shaped body
// carrying `message` but no status or detail. Proxies and Go's default mux answer with plain text
// ("Bad Gateway", "404 page not found"), which the generated client throws as a bare string.
// HTML error pages fall back to the status text.
export function normalizeProblemBody(error: unknown, response: Response | undefined): unknown {
	if (!response || response.ok || error instanceof Error) {
		return error
	}
	const body: Record<string, unknown> = isWireProblem(error) ? error : {}
	return new ApiError({
		...body as VikunjaErrorModel,
		status: response.status,
		detail: nonEmptyString(body.detail)
			?? nonEmptyString(body.message)
			?? plainText(error)
			?? (response.statusText || `HTTP ${response.status}`),
	})
}
