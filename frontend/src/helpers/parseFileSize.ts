// Binary multipliers, as the API parses files.maxsize (datasize: 1 KB = 1024 B).
const UNITS: Record<string, number> = {
	'': 1,
	b: 1,
	k: 1024,
	m: 1024 ** 2,
	g: 1024 ** 3,
	t: 1024 ** 4,
	p: 1024 ** 5,
}

/**
 * Parses a size like the API's max_file_size ("20MB", "1 GB", "500kb") into bytes.
 * Returns null for anything it does not understand, so callers skip the check
 * instead of rejecting every file.
 */
export function parseFileSize(input: string | undefined | null): number | null {
	const match = /^\s*(\d+(?:\.\d+)?)\s*([bkmgtp]?)(?:i?b)?\s*$/i.exec(input ?? '')
	if (!match) {
		return null
	}
	const bytes = Number(match[1]) * UNITS[match[2].toLowerCase()]
	return Number.isFinite(bytes) && bytes > 0 ? bytes : null
}
