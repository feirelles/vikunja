import {defineConfig, devices} from '@playwright/test'
import {execSync} from 'child_process'

// Browser binary: PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH when set (any Chromium-based
// binary, e.g. /usr/bin/google-chrome), else `which chromium`, else Playwright's own
// downloaded browser.
const getChromiumPath = () => {
	if (process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH) {
		return process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
	}
	try {
		return execSync('which chromium', {encoding: 'utf-8'}).trim()
	} catch {
		return undefined
	}
}

export default defineConfig({
	testDir: './tests/e2e',
	fullyParallel: false,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	workers: 1, // No parallelization initially
	reporter: process.env.CI ? [['html'], ['list']] : 'html',
	use: {
		baseURL: process.env.BASE_URL || 'http://127.0.0.1:4173',
		trace: 'on-first-retry',
		screenshot: 'only-on-failure',
		testIdAttribute: 'data-cy', // Preserve existing data-cy selectors
		serviceWorkers: 'block',
		launchOptions: {
			executablePath: getChromiumPath(),
		},
	},
	projects: [
		{
			name: 'chromium',
			use: {...devices['Desktop Chrome']},
		},
	],
	// webServer configuration removed - we manually start services in CI
	// For local development, run `pnpm preview` and `pnpm preview:vikunja` separately
})
