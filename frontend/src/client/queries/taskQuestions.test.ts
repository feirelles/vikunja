import {
	beforeEach,
	expect,
	it,
	vi,
} from 'vitest'
import {QueryClient} from '@tanstack/vue-query'
import {
	answerTaskQuestionMutationOptions,
	normalizeTaskQuestion,
	taskQuestionKeys,
	taskQuestionsQuery,
	type TaskQuestionResponse,
} from './taskQuestions'
import {
	normalizeTask,
	taskKeys,
	type TaskResponse,
} from './tasks'

const sdk = vi.hoisted(() => ({
	taskQuestionsList: vi.fn(),
	taskQuestionsAnswer: vi.fn(),
}))
vi.mock('@/client/generated', () => sdk)
const message = vi.hoisted(() => ({
	error: vi.fn(),
	success: vi.fn(),
}))
vi.mock('@/message', () => message)

let client: QueryClient
beforeEach(() => {
	vi.clearAllMocks()
	client = new QueryClient({defaultOptions: {queries: {retry: false}}})
})

function question(id: number, status: TaskQuestionResponse['status'] = 'open') {
	return normalizeTaskQuestion({
		id,
		task_id: 1,
		title: `Question ${id}`,
		status,
		options: [{
			key: 'A',
			label: 'Yes',
		}],
	})
}

function answer(input: {questionId: number, answer: {option?: string, other?: string, notes?: string, defer?: boolean}}) {
	return client.getMutationCache().build(client, answerTaskQuestionMutationOptions()).execute({
		taskId: 1,
		...input,
	})
}

it('fetches every page of a task\'s questions and normalizes them', async () => {
	sdk.taskQuestionsList
		.mockResolvedValueOnce({data: {
			items: [{id: 1}],
			page: 1,
			total_pages: 2,
		}})
		.mockResolvedValueOnce({data: {
			items: [{
				id: 2,
				options: [{key: 'A'}],
			}],
			page: 2,
			total_pages: 2,
		}})

	const result = await client.fetchQuery(taskQuestionsQuery(1))

	expect(result).toEqual([
		{
			id: 1,
			task_id: 0,
			title: '',
			summary: '',
			details: '',
			options: [],
			status: 'open',
			answer_option: '',
			answer_other: '',
			answer_notes: '',
		},
		expect.objectContaining({
			id: 2,
			options: [{
				key: 'A',
				label: '',
			}],
		}),
	])
	expect(sdk.taskQuestionsList).toHaveBeenNthCalledWith(2, {
		path: {task: 1},
		query: {page: 2},
		signal: expect.any(AbortSignal),
	})
})

it('does not query a task without an id', () => {
	expect(taskQuestionsQuery(0).enabled).toBe(false)
})

it('sends the answer and replaces the question in the cached list', async () => {
	client.setQueryData(taskQuestionKeys.list(1), [question(1), question(2)])
	sdk.taskQuestionsAnswer.mockResolvedValue({data: {
		...question(1),
		status: 'answered',
		answer_option: 'A',
		answer_notes: 'go',
	}})

	await answer({
		questionId: 1,
		answer: {
			option: 'A',
			notes: 'go',
		},
	})

	expect(sdk.taskQuestionsAnswer).toHaveBeenCalledWith({
		path: {
			task: 1,
			question: 1,
		},
		body: {
			option: 'A',
			notes: 'go',
		},
	})
	expect(client.getQueryData<TaskQuestionResponse[]>(taskQuestionKeys.list(1))).toEqual([
		expect.objectContaining({
			id: 1,
			status: 'answered',
			answer_option: 'A',
			answer_notes: 'go',
		}),
		question(2),
	])
	expect(message.success).toHaveBeenCalledWith({message: 'The question was answered.'})
})

it('decrements the open question count of every cached copy when an open question is answered', async () => {
	client.setQueryData(taskQuestionKeys.list(1), [question(1)])
	client.setQueryData(taskKeys.detail(1), normalizeTask({
		id: 1,
		open_question_count: 2,
	}))
	client.setQueryData(taskKeys.allList({project: 3}), [normalizeTask({
		id: 1,
		open_question_count: 2,
	})])
	sdk.taskQuestionsAnswer.mockResolvedValue({data: {
		...question(1),
		status: 'deferred',
	}})

	await answer({
		questionId: 1,
		answer: {defer: true},
	})

	expect(client.getQueryData<TaskResponse>(taskKeys.detail(1))?.open_question_count).toBe(1)
	expect(client.getQueryData<TaskResponse[]>(taskKeys.allList({project: 3}))?.[0]?.open_question_count).toBe(1)
	expect(message.success).toHaveBeenCalledWith({message: 'The question was deferred.'})
})

it('keeps the open question count when a deferred question is answered', async () => {
	client.setQueryData(taskQuestionKeys.list(1), [question(1, 'deferred')])
	client.setQueryData(taskKeys.detail(1), normalizeTask({
		id: 1,
		open_question_count: 1,
	}))
	sdk.taskQuestionsAnswer.mockResolvedValue({data: {
		...question(1),
		status: 'answered',
		answer_other: 'something else',
	}})

	await answer({
		questionId: 1,
		answer: {other: 'something else'},
	})

	expect(client.getQueryData<TaskResponse>(taskKeys.detail(1))?.open_question_count).toBe(1)
})

it('does not materialize a question list nobody loaded', async () => {
	sdk.taskQuestionsAnswer.mockResolvedValue({data: {
		...question(1),
		status: 'answered',
	}})

	await answer({
		questionId: 1,
		answer: {option: 'A'},
	})

	expect(client.getQueryData(taskQuestionKeys.list(1))).toBeUndefined()
})

it('invalidates the question list and task lists after an answer', async () => {
	client.setQueryData(taskQuestionKeys.list(1), [question(1)])
	client.setQueryData(taskKeys.allList({project: 3}), [normalizeTask({id: 1})])
	sdk.taskQuestionsAnswer.mockResolvedValue({data: {
		...question(1),
		status: 'answered',
	}})

	await answer({
		questionId: 1,
		answer: {option: 'A'},
	})

	expect(client.getQueryState(taskQuestionKeys.list(1))?.isInvalidated).toBe(true)
	expect(client.getQueryState(taskKeys.allList({project: 3}))?.isInvalidated).toBe(true)
})

it('refetches the list when the answer fails, e.g. because someone answered it first', async () => {
	client.setQueryData(taskQuestionKeys.list(1), [question(1)])
	sdk.taskQuestionsAnswer.mockRejectedValue(new Error('412'))

	await expect(answer({
		questionId: 1,
		answer: {option: 'A'},
	})).rejects.toThrow('412')

	expect(client.getQueryState(taskQuestionKeys.list(1))?.isInvalidated).toBe(true)
	expect(message.error).toHaveBeenCalled()
})
