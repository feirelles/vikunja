import {expect, it, vi} from 'vitest'
import {mount} from '@vue/test-utils'

vi.mock('vue-i18n', async importOriginal => ({
	...(await importOriginal<typeof import('vue-i18n')>()),
	useI18n: () => ({t: (key: string, count: number) => `${key}:${count}`}),
}))

import QuestionCount from './QuestionCount.vue'

it('shows the pluralized count and labels it as open questions', () => {
	const wrapper = mount(QuestionCount, {
		props: {count: 3},
		global: {
			stubs: {Icon: true},
			directives: {tooltip: {}},
			mocks: {$t: (key: string, count: number) => `${key}:${count}`},
		},
	})

	expect(wrapper.text()).toBe('task.questions.count:3')
	expect(wrapper.attributes('role')).toBe('img')
	expect(wrapper.attributes('aria-label')).toBe('task.questions.openCount:3')
})
