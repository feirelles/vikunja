// @vitest-environment jsdom
// happy-dom breaks DOMPurify, which renders the question details
import {afterEach, describe, expect, it, vi} from 'vitest'
import {createPinia} from 'pinia'
import {flushPromises, mount, type VueWrapper} from '@vue/test-utils'
import {QueryClient, VueQueryPlugin} from '@tanstack/vue-query'
import BaseButton from '@/components/base/BaseButton.vue'
import XButton from '@/components/input/Button.vue'
import type {TaskQuestion} from '@/client/generated'

const sdk = vi.hoisted(() => ({
	taskQuestionsList: vi.fn(),
	taskQuestionsAnswer: vi.fn(),
}))

vi.mock('@/client/generated', () => sdk)

vi.mock('vue-i18n', async importOriginal => ({
	...(await importOriginal<typeof import('vue-i18n')>()),
	useI18n: () => ({t: (key: string) => key}),
}))

vi.mock('@/message', () => ({error: vi.fn(), success: vi.fn()}))

import TaskQuestions from './TaskQuestions.vue'

const openQuestion: TaskQuestion = {
	id: 1,
	task_id: 1,
	title: 'Which database?',
	summary: 'We need to pick one',
	details: '<p>Some <strong>context</strong></p><img src="x" onerror="alert(1)">',
	status: 'open',
	options: [
		{
			key: 'A',
			label: 'Postgres',
			description: 'Solid',
		},
		{
			key: 'B',
			label: 'SQLite',
		},
	],
	recommendation: {
		key: 'A',
		reason: 'We already run it',
	},
}

const deferredQuestion: TaskQuestion = {
	id: 2,
	task_id: 1,
	title: 'Ship on Friday?',
	status: 'deferred',
	options: [{
		key: 'Y',
		label: 'Yes',
	}],
	answered_by: {
		id: 1,
		username: 'felipe',
	},
}

const answeredQuestion: TaskQuestion = {
	id: 3,
	task_id: 1,
	title: 'Use dark mode?',
	status: 'answered',
	options: [{
		key: 'Y',
		label: 'Yes please',
	}],
	answer_option: 'Y',
	answer_notes: 'Thanks',
	answered_by: {
		id: 1,
		username: 'felipe',
	},
}

const cancelledQuestion: TaskQuestion = {
	id: 4,
	task_id: 1,
	title: 'Cancelled one',
	status: 'cancelled',
}

const mounted: VueWrapper[] = []

async function mountQuestions(questions: TaskQuestion[], canWrite = true) {
	sdk.taskQuestionsList.mockResolvedValue({data: {
		items: questions,
		page: 1,
		total_pages: 1,
	}})
	const queryClient = new QueryClient({defaultOptions: {queries: {retry: false}}})
	const wrapper = mount(TaskQuestions, {
		props: {
			taskId: 1,
			canWrite,
		},
		global: {
			plugins: [createPinia(), [VueQueryPlugin, {queryClient}]],
			components: {XButton, BaseButton},
			stubs: {Icon: true},
			directives: {tooltip: {}},
			mocks: {$t: (key: string) => key},
		},
	})
	mounted.push(wrapper)
	await flushPromises()
	return wrapper
}

function question(wrapper: VueWrapper, id: number) {
	return wrapper.get(`[data-question-id="${id}"]`)
}

function lastAnswerBody() {
	return sdk.taskQuestionsAnswer.mock.lastCall?.[0]
}

afterEach(() => {
	mounted.splice(0).forEach(wrapper => wrapper.unmount())
	sdk.taskQuestionsList.mockReset()
	sdk.taskQuestionsAnswer.mockReset()
})

describe('TaskQuestions', () => {
	it('renders nothing when the task has no visible questions', async () => {
		const wrapper = await mountQuestions([cancelledQuestion])

		expect(wrapper.find('.task-questions').exists()).toBe(false)
	})

	it('shows open and deferred questions with their options and hides cancelled ones', async () => {
		const wrapper = await mountQuestions([openQuestion, deferredQuestion, answeredQuestion, cancelledQuestion])

		const open = question(wrapper, 1)
		expect(open.text()).toContain('Which database?')
		expect(open.text()).toContain('We need to pick one')
		expect(open.findAll('.question-option').map(o => o.attributes('data-option-key'))).toEqual(['A', 'B'])
		expect(open.find('.tag.is-warning').exists()).toBe(false)

		const deferred = question(wrapper, 2)
		expect(deferred.get('.tag.is-warning').text()).toBe('task.questions.deferred')
		expect(deferred.find('.question-option').exists()).toBe(true)
		expect(deferred.find('.defer-button').exists()).toBe(false)

		expect(wrapper.find('[data-question-id="4"]').exists()).toBe(false)
	})

	it('highlights the recommended option with its reason', async () => {
		const wrapper = await mountQuestions([openQuestion])

		const recommended = wrapper.get('[data-option-key="A"]')
		expect(recommended.classes()).toContain('is-recommended')
		expect(recommended.get('.tag.is-primary').text()).toBe('task.questions.recommended')
		expect(recommended.text()).toContain('We already run it')
		expect(wrapper.get('[data-option-key="B"]').classes()).not.toContain('is-recommended')
	})

	it('keeps details collapsed until toggled and sanitizes them', async () => {
		const wrapper = await mountQuestions([openQuestion])

		expect(wrapper.find('.question-details').exists()).toBe(false)
		await wrapper.get('.details-toggle').trigger('click')

		const details = wrapper.get('.question-details')
		expect(details.html()).toContain('<strong>context</strong>')
		expect(details.html()).not.toContain('onerror')
	})

	it('collapses answered questions to one line with the chosen option and expands them', async () => {
		const wrapper = await mountQuestions([answeredQuestion])

		const answered = question(wrapper, 3)
		expect(answered.get('.answered-line').text()).toContain('Use dark mode?')
		expect(answered.get('.answered-value').text()).toBe('Yes please')
		expect(answered.find('.question-option').exists()).toBe(false)
		expect(answered.find('.answer-notes').exists()).toBe(false)

		await answered.get('.answered-line').trigger('click')

		expect(answered.get('.answer-notes').text()).toBe('Thanks')
		expect(answered.get('.answer-meta').text()).toBe('task.questions.answeredBy')
	})

	it('sends the option and notes when an option is clicked', async () => {
		sdk.taskQuestionsAnswer.mockResolvedValue({data: {
			...openQuestion,
			status: 'answered',
			answer_option: 'B',
		}})
		const wrapper = await mountQuestions([openQuestion])

		await wrapper.get('#task-question-1-notes').setValue('  go for it ')
		await wrapper.get('[data-option-key="B"]').trigger('click')
		await flushPromises()

		expect(lastAnswerBody()).toEqual({
			path: {
				task: 1,
				question: 1,
			},
			body: {
				option: 'B',
				notes: 'go for it',
			},
		})
	})

	it('sends the other answer and notes from the other field', async () => {
		sdk.taskQuestionsAnswer.mockResolvedValue({data: {
			...deferredQuestion,
			status: 'answered',
			answer_other: 'MySQL',
		}})
		const wrapper = await mountQuestions([deferredQuestion])

		await wrapper.get('#task-question-2-other').setValue('MySQL')
		await wrapper.get('#task-question-2-notes').setValue('legacy')
		await wrapper.get('.send-other').trigger('click')
		await flushPromises()

		expect(lastAnswerBody()?.body).toEqual({
			other: 'MySQL',
			notes: 'legacy',
		})
	})

	it('does not send an empty other answer', async () => {
		const wrapper = await mountQuestions([openQuestion])

		await wrapper.get('#task-question-1-other').setValue('   ')
		await wrapper.get('.send-other').trigger('click')
		await flushPromises()

		expect(sdk.taskQuestionsAnswer).not.toHaveBeenCalled()
	})

	it('defers with the notes', async () => {
		sdk.taskQuestionsAnswer.mockResolvedValue({data: {
			...openQuestion,
			status: 'deferred',
		}})
		const wrapper = await mountQuestions([openQuestion])

		await wrapper.get('#task-question-1-notes').setValue('next sprint')
		await wrapper.get('.defer-button').trigger('click')
		await flushPromises()

		expect(lastAnswerBody()?.body).toEqual({
			defer: true,
			notes: 'next sprint',
		})
	})

	it('ignores further answers while one is pending', async () => {
		sdk.taskQuestionsAnswer.mockReturnValue(new Promise(() => {}))
		const wrapper = await mountQuestions([openQuestion])

		await wrapper.get('[data-option-key="A"]').trigger('click')
		await flushPromises()

		expect(wrapper.get('[data-option-key="B"]').attributes('aria-disabled')).toBe('true')
		await wrapper.get('[data-option-key="B"]').trigger('click')
		await wrapper.get('.defer-button').trigger('click')
		await flushPromises()

		expect(sdk.taskQuestionsAnswer).toHaveBeenCalledOnce()
	})

	it('shows the options but no answer controls without edit rights', async () => {
		const wrapper = await mountQuestions([openQuestion, deferredQuestion], false)

		expect(wrapper.findAll('.question-option')).toHaveLength(3)
		expect(wrapper.find('button.question-option').exists()).toBe(false)
		expect(wrapper.find('.question-controls').exists()).toBe(false)
		expect(wrapper.find('.defer-button').exists()).toBe(false)

		await wrapper.get('[data-option-key="A"]').trigger('click')
		expect(sdk.taskQuestionsAnswer).not.toHaveBeenCalled()
	})
})
