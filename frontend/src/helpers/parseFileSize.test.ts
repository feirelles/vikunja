import {describe, expect, it} from 'vitest'

import {parseFileSize} from './parseFileSize'

describe('parseFileSize', () => {
	it('parses the API default', () => {
		expect(parseFileSize('20MB')).toBe(20 * 1024 ** 2)
	})

	it('accepts any case, spaces and short or binary units', () => {
		expect(parseFileSize('1 gb')).toBe(1024 ** 3)
		expect(parseFileSize('500k')).toBe(500 * 1024)
		expect(parseFileSize('2MiB')).toBe(2 * 1024 ** 2)
		expect(parseFileSize('1.5 MB')).toBe(1.5 * 1024 ** 2)
	})

	it('treats a bare number or B as bytes', () => {
		expect(parseFileSize('1024')).toBe(1024)
		expect(parseFileSize('10B')).toBe(10)
	})

	it('returns null for values it cannot parse', () => {
		expect(parseFileSize('')).toBeNull()
		expect(parseFileSize(undefined)).toBeNull()
		expect(parseFileSize('lots')).toBeNull()
		expect(parseFileSize('0MB')).toBeNull()
		expect(parseFileSize('20XB')).toBeNull()
	})
})
