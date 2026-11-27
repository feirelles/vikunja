import {
	queryOptions,
	useMutation,
	type QueryClient,
} from '@tanstack/vue-query'
import {
	taskQuestionsAnswer,
	taskQuestionsList,
} from '@/client/generated'
import type {
	TaskQuestion,
	TaskQuestionAnswerWritable,
	TaskQuestionOption,
} from '@/client/generated'
import {contextMutationOptions} from './contextMutation'
import {fetchAllPages} from './fetchAllPages'
import {
	invalidateTaskMembership,
	mapTaskEverywhere,
} from './taskCache'
import {i18n} from '@/i18n'

export type TaskQuestionStatus = NonNullable<TaskQuestion['status']>
export type TaskQuestionOptionResponse = {
	key: string,
	label: string,
	description?: string,
}
export type TaskQuestionResponse = Omit<TaskQuestion,
	'id' |
	'task_id' |
	'title' |
	'summary' |
	'details' |
	'options' |
	'status' |
	'answer_option' |
	'answer_other' |
	'answer_notes'
> & {
	id: number,
	task_id: number,
	title: string,
	summary: string,
	details: string,
	options: TaskQuestionOptionResponse[],
	status: TaskQuestionStatus,
	answer_option: string,
	answer_other: string,
	answer_notes: string,
}

export const taskQuestionKeys = {
	list: (taskId: number) => ['task-questions', taskId] as const,
}

function normalizeOption(option: TaskQuestionOption): TaskQuestionOptionResponse {
	return {
		...option,
		key: option.key ?? '',
		label: option.label ?? '',
	}
}

export function normalizeTaskQuestion(question: TaskQuestion): TaskQuestionResponse {
	return {
		...question,
		id: question.id ?? 0,
		task_id: question.task_id ?? 0,
		title: question.title ?? '',
		summary: question.summary ?? '',
		details: question.details ?? '',
		options: (question.options ?? []).map(normalizeOption),
		status: question.status ?? 'open',
		answer_option: question.answer_option ?? '',
		answer_other: question.answer_other ?? '',
		answer_notes: question.answer_notes ?? '',
	}
}

export function taskQuestionsQuery(taskId: number) {
	return queryOptions({
		queryKey: taskQuestionKeys.list(taskId),
		enabled: taskId > 0,
		queryFn: async ({signal}) => (await fetchAllPages(async page => (await taskQuestionsList({
			path: {task: taskId},
			query: {page},
			signal,
		})).data)).map(normalizeTaskQuestion),
	})
}

export function answerTaskQuestionMutationOptions() {
	return contextMutationOptions({
		mutationFn: async ({taskId, questionId, answer}: {
			taskId: number,
			questionId: number,
			answer: TaskQuestionAnswerWritable,
		}) =>
			(await taskQuestionsAnswer({
				path: {
					task: taskId,
					question: questionId,
				},
				body: answer,
			})).data,
		onSuccess: (answered, {taskId, questionId}, client) => {
			const previous = client.getQueryData<TaskQuestionResponse[]>(taskQuestionKeys.list(taskId))
				?.find(question => question.id === questionId)
			client.setQueryData<TaskQuestionResponse[]>(taskQuestionKeys.list(taskId), current => current?.map(
				question => question.id === questionId ? normalizeTaskQuestion(answered) : question,
			))
			if (previous?.status !== 'open' || answered.status === 'open') return
			mapTaskEverywhere(client, taskId, task => ({
				...task,
				open_question_count: task.open_question_count === undefined
					? undefined
					: Math.max(0, task.open_question_count - 1),
			}))
		},
		onSettled: ({taskId}, client) => settle(client, taskId),
		successMessage: (_data, {answer}) => answer.defer
			? i18n.global.t('task.questions.deferSuccess')
			: i18n.global.t('task.questions.answerSuccess'),
	})
}

function settle(client: QueryClient, taskId: number) {
	return Promise.all([
		client.invalidateQueries({queryKey: taskQuestionKeys.list(taskId)}),
		invalidateTaskMembership(client, taskId),
	])
}

export function useAnswerTaskQuestionMutation() {
	return useMutation(answerTaskQuestionMutationOptions())
}
