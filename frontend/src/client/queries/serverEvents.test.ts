import {
	expect,
	it,
	vi,
} from 'vitest'
import {
	QueryClient,
	QueryObserver,
} from '@tanstack/vue-query'
import {
	parseProjectTaskEvent,
	parseServerCacheEvent,
	serverCacheEventMutationOptions,
	type ProjectTaskEvent,
} from './serverEvents'
import {ApiError} from '@/client/problem'
import {projectKeys} from './projects'
import {normalizeBucket, type BoardData} from './kanban'
import {
	timeEntryKeys,
	normalizeTimeEntry,
	type TimeEntryPage,
	type TimeEntryResponse,
} from './timeEntries'
import {commentKeys} from './comments'
import {notificationKeys} from './notifications'
import {
	taskKeys,
	normalizeTask,
	type TaskResponse,
} from './tasks'
import {kanbanKeys} from './kanban'
vi.mock('@/message', () => ({
	error: vi.fn(),
	success: vi.fn(),
}))
const sdk = vi.hoisted(() => ({tasksRead: vi.fn()}))
vi.mock('@/client/generated', async importOriginal => ({
	...await importOriginal<typeof import('@/client/generated')>(),
	tasksRead: sdk.tasksRead,
}))

const RUNNING_ENTRY = normalizeTimeEntry({
	id: 4,
	user: {id: 7},
	start_time: '2026-09-19T08:00:00Z',
	end_time: null,
})

const TASK_ENTRY = normalizeTimeEntry({
	id: 9,
	user: {id: 7},
	task_id: 3,
	start_time: '2026-09-19T08:00:00Z',
	end_time: '2026-09-19T09:00:00Z',
})

function timeEntryPage(items: TimeEntryResponse[]): TimeEntryPage {
	return {
		items,
		page: 1,
		per_page: 50,
		total: items.length,
		total_pages: 1,
	}
}

it('ignores malformed payloads', () => {
	expect(parseServerCacheEvent('timer.created', undefined, 7)).toBeNull()
	expect(parseServerCacheEvent('notification.created', 'task.comment', 7)).toBeNull()
	expect(parseServerCacheEvent('notification.created', {name: 'team.member.added'}, 7)).toBeNull()
	expect(parseServerCacheEvent('notification.created', {
		id: 0,
		name: 'team.member.added',
	}, 7)).toBeNull()
})

it('rejects timer entries that do not belong to the current user', () => {
	const entry = {
		id: 4,
		user: {id: 8},
		task_id: 1,
	}
	expect(parseServerCacheEvent('timer.created', entry, 7)).toBeNull()
	expect(parseServerCacheEvent('timer.created', entry, undefined)).toBeNull()
	expect(parseServerCacheEvent('timer.created', {
		id: 4,
		task_id: 1,
	}, undefined)).toBeNull()
	expect(parseServerCacheEvent('timer.created', {
		...entry,
		user: {id: 7},
	}, 7)).not.toBeNull()
})

it('rejects timer entries with an unusable id or task id', () => {
	expect(parseServerCacheEvent('timer.created', {
		id: 0,
		user: {id: 7},
	}, 7)).toBeNull()
	expect(parseServerCacheEvent('timer.created', {
		id: 4,
		user: {id: 7},
		task_id: '1',
	}, 7)).toBeNull()
})

it('falls back to an inbox refresh for notifications it cannot route', () => {
	expect(parseServerCacheEvent('notification.created', {
		id: 5,
		name: 'team.member.added',
	}, 7)).toEqual({kind: 'notifications'})
	expect(parseServerCacheEvent('notification.created', {
		id: 5,
		name: 'task.comment',
		notification: {},
	}, 7)).toEqual({kind: 'notifications'})
	expect(parseServerCacheEvent('notification.created', {
		id: 5,
		name: 'task.comment',
		notification: {task: {id: 0}},
	}, 7)).toEqual({kind: 'notifications'})
})

it.each([
	'notification',
	'comment',
	'reconnect',
	'subscribe',
])('invalidates notification data on %s without replacing it with the event payload', async kind => {
	const client = new QueryClient()
	client.setQueryData(notificationKeys.all, [{id: 1}], {updatedAt: 10})
	const event = kind === 'reconnect'
		? {kind: 'reconnect'} as const
		: kind === 'subscribe'
			? {
				kind: 'subscribed',
				since: 20,
			} as const
			: parseServerCacheEvent('notification.created', {
				id: 2,
				name: kind === 'comment' ? 'task.comment' : 'team.member.added',
				notification: {task: {id: 3}},
			}, 7)!
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(event)
	expect(client.getQueryData(notificationKeys.all)).toEqual([{id: 1}])
	expect(client.getQueryState(notificationKeys.all)?.isInvalidated).toBe(true)
})

it('invalidates only the notified task comments and detail', async () => {
	const client = new QueryClient()
	client.setQueryData(commentKeys.page(1, 'asc', 1), {items: []})
	client.setQueryData(commentKeys.page(2, 'asc', 1), {items: []})
	client.setQueryData(taskKeys.detail(1), normalizeTask({id: 1}))
	const event = parseServerCacheEvent('notification.created', {
		name: 'task.comment',
		notification: {task: {id: 1}},
	}, 7)!
	expect(event).toEqual({
		kind: 'notifications',
		taskId: 1,
	})
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(event)
	expect(client.getQueryState(commentKeys.page(1, 'asc', 1))?.isInvalidated).toBe(true)
	expect(client.getQueryState(commentKeys.page(2, 'asc', 1))?.isInvalidated).toBe(false)
	expect(client.getQueryState(taskKeys.detail(1))?.isInvalidated).toBe(true)
})

it('stales task collections only for a task they hold', async () => {
	const client = new QueryClient()
	const listKey = taskKeys.allList({project: 2})
	const queryFn = vi.fn(() => [normalizeTask({id: 5})])
	const unsubscribe = new QueryObserver(client, {
		queryKey: listKey,
		queryFn,
	}).subscribe(() => {})
	await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1))
	client.setQueryData(taskKeys.detail(5), normalizeTask({id: 5}))
	const commentOn = (taskId: number) => parseServerCacheEvent('notification.created', {
		name: 'task.comment',
		notification: {task: {id: taskId}},
	}, 7)!
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(commentOn(1))
	expect(client.getQueryState(listKey)?.isInvalidated).toBe(false)
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(commentOn(5))
	expect(client.getQueryState(listKey)?.isInvalidated).toBe(true)
	expect(client.getQueryState(listKey)?.fetchStatus).toBe('idle')
	expect(queryFn).toHaveBeenCalledTimes(1)
	expect(client.getQueryState(taskKeys.detail(5))?.isInvalidated).toBe(true)
	unsubscribe()
})

it('does not stale task collections for an open task none of them hold', async () => {
	const client = new QueryClient()
	const listKey = taskKeys.allList({project: 2})
	client.setQueryData(listKey, [normalizeTask({id: 5})])
	client.setQueryData(taskKeys.detail(9), normalizeTask({id: 9}))
	const event = parseServerCacheEvent('notification.created', {
		name: 'task.comment',
		notification: {task: {id: 9}},
	}, 7)!
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(event)
	expect(client.getQueryState(listKey)?.isInvalidated).toBe(false)
	expect(client.getQueryState(taskKeys.detail(9))?.isInvalidated).toBe(true)
})

it('refetches an open list for a created timer without inserting it or refetching the active key', async () => {
	const client = new QueryClient()
	const listKey = timeEntryKeys.list('task_id = 99', 'UTC', 1)
	const queryFn = vi.fn(() => timeEntryPage([]))
	const activeFn = vi.fn(() => null)
	const unsubscribe = new QueryObserver(client, {
		queryKey: listKey,
		queryFn,
	}).subscribe(() => {})
	const unsubscribeActive = new QueryObserver(client, {
		queryKey: timeEntryKeys.active(7),
		queryFn: activeFn,
	}).subscribe(() => {})
	await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1))
	await vi.waitFor(() => expect(activeFn).toHaveBeenCalledTimes(1))
	const event = parseServerCacheEvent('timer.created', RUNNING_ENTRY, 7)!
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(event)
	expect(client.getQueryData(timeEntryKeys.active(7))).toEqual(RUNNING_ENTRY)
	expect(client.getQueryData<TimeEntryPage>(listKey)?.items).toEqual([])
	expect(queryFn).toHaveBeenCalledTimes(2)
	expect(activeFn).toHaveBeenCalledTimes(1)
	unsubscribe()
	unsubscribeActive()
})

it('patches an updated timer into an open list without refetching it', async () => {
	const client = new QueryClient()
	const listKey = timeEntryKeys.list('task_id = 99', 'UTC', 1)
	const queryFn = vi.fn(() => timeEntryPage([RUNNING_ENTRY]))
	const unsubscribe = new QueryObserver(client, {
		queryKey: listKey,
		queryFn,
	}).subscribe(() => {})
	await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1))
	client.setQueryData(timeEntryKeys.active(7), RUNNING_ENTRY)
	const stopped = normalizeTimeEntry({
		...RUNNING_ENTRY,
		end_time: '2026-09-19T09:00:00Z',
	})
	const event = parseServerCacheEvent('timer.updated', stopped, 7)!
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(event)
	expect(client.getQueryData<TimeEntryPage>(listKey)?.items).toEqual([stopped])
	expect(client.getQueryData(timeEntryKeys.active(7))).toBeNull()
	expect(client.getQueryState(listKey)?.isInvalidated).toBe(false)
	expect(queryFn).toHaveBeenCalledTimes(1)
	unsubscribe()
})

it('invalidates both tasks when an updated timer moves to another task', async () => {
	const client = new QueryClient()
	client.setQueryData(timeEntryKeys.list('task_id = 3', 'UTC', 1), timeEntryPage([TASK_ENTRY]))
	client.setQueryData(taskKeys.detail(3), normalizeTask({id: 3}))
	client.setQueryData(taskKeys.detail(4), normalizeTask({id: 4}))
	const event = parseServerCacheEvent('timer.updated', {
		...TASK_ENTRY,
		task_id: 4,
	}, 7)!
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(event)
	expect(client.getQueryState(taskKeys.detail(3))?.isInvalidated).toBe(true)
	expect(client.getQueryState(taskKeys.detail(4))?.isInvalidated).toBe(true)
})

it('invalidates only the timer task when an update keeps it there', async () => {
	const client = new QueryClient()
	client.setQueryData(timeEntryKeys.list('task_id = 3', 'UTC', 1), timeEntryPage([TASK_ENTRY]))
	client.setQueryData(taskKeys.detail(3), normalizeTask({id: 3}))
	client.setQueryData(taskKeys.detail(4), normalizeTask({id: 4}))
	const event = parseServerCacheEvent('timer.updated', {
		...TASK_ENTRY,
		comment: 'changed',
	}, 7)!
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(event)
	expect(client.getQueryState(taskKeys.detail(3))?.isInvalidated).toBe(true)
	expect(client.getQueryState(taskKeys.detail(4))?.isInvalidated).toBe(false)
})

it('invalidates only the new task when the updated timer is not cached', async () => {
	const client = new QueryClient()
	client.setQueryData(taskKeys.detail(3), normalizeTask({id: 3}))
	client.setQueryData(taskKeys.detail(4), normalizeTask({id: 4}))
	const event = parseServerCacheEvent('timer.updated', {
		...TASK_ENTRY,
		task_id: 4,
	}, 7)!
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(event)
	expect(client.getQueryState(taskKeys.detail(3))?.isInvalidated).toBe(false)
	expect(client.getQueryState(taskKeys.detail(4))?.isInvalidated).toBe(true)
})

it('removes a deleted timer from an open list without refetching it', async () => {
	const client = new QueryClient()
	const listKey = timeEntryKeys.list('task_id = 99', 'UTC', 1)
	const queryFn = vi.fn(() => timeEntryPage([RUNNING_ENTRY]))
	const unsubscribe = new QueryObserver(client, {
		queryKey: listKey,
		queryFn,
	}).subscribe(() => {})
	await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1))
	client.setQueryData(timeEntryKeys.active(7), RUNNING_ENTRY)
	const event = parseServerCacheEvent('timer.deleted', RUNNING_ENTRY, 7)!
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(event)
	expect(client.getQueryData<TimeEntryPage>(listKey)?.items).toEqual([])
	expect(client.getQueryData<TimeEntryPage>(listKey)?.total).toBe(0)
	expect(client.getQueryData(timeEntryKeys.active(7))).toBeNull()
	expect(client.getQueryState(listKey)?.isInvalidated).toBe(false)
	expect(queryFn).toHaveBeenCalledTimes(1)
	unsubscribe()
})

it('refetches only cache families that loaded before the first subscription', async () => {
	const client = new QueryClient()
	const listKey = timeEntryKeys.list('task_id = 99', 'UTC', 1)
	const queryFn = vi.fn(() => timeEntryPage([]))
	const unsubscribe = new QueryObserver(client, {
		queryKey: listKey,
		queryFn,
	}).subscribe(() => {})
	await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1))
	const since = Date.now() + 1000
	const commentKey = commentKeys.page(1, 'asc', 1)
	const boardKey = kanbanKeys.board(1, 1)
	client.setQueryData(commentKey, {items: []}, {updatedAt: since - 1})
	client.setQueryData(taskKeys.detail(1), normalizeTask({id: 1}), {updatedAt: since + 1})
	client.setQueryData(boardKey, {buckets: []}, {updatedAt: since - 1})
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute({
		kind: 'subscribed',
		since,
	})
	expect(queryFn).toHaveBeenCalledTimes(2)
	expect(client.getQueryState(commentKey)?.isInvalidated).toBe(true)
	expect(client.getQueryState(taskKeys.detail(1))?.isInvalidated).toBe(false)
	expect(client.getQueryState(boardKey)?.isInvalidated).toBe(false)
	unsubscribe()
})

it('leaves a query still loading at the first subscription untouched', async () => {
	const client = new QueryClient()
	const listKey = timeEntryKeys.list('task_id = 1', 'UTC', 1)
	const queryFn = vi.fn(() => new Promise<TimeEntryPage>(() => {}))
	const unsubscribe = new QueryObserver(client, {
		queryKey: listKey,
		queryFn,
	}).subscribe(() => {})
	await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1))
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute({
		kind: 'subscribed',
		since: Date.now() + 1000,
	})
	expect(queryFn).toHaveBeenCalledTimes(1)
	expect(client.getQueryState(listKey)?.isInvalidated).toBe(false)
	unsubscribe()
})

it('refreshes task details and marks board membership stale after reconnecting', async () => {
	const client = new QueryClient()
	client.setQueryData(taskKeys.detail(1), normalizeTask({
		id: 1,
		comment_count: 0,
	}))
	const boardKey = kanbanKeys.board(1, 1)
	client.setQueryData(boardKey, {buckets: []})
	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute({kind: 'reconnect'})
	expect(client.getQueryState(boardKey)?.isInvalidated).toBe(true)
	expect(client.getQueryState(taskKeys.detail(1))?.isInvalidated).toBe(true)
})

it('parses a project task frame and rejects malformed ones', () => {
	expect(parseProjectTaskEvent('task.updated', {
		project_id: 1,
		task_id: 9,
		doer_id: 7,
		old_project_id: 2,
		placements: [
			{project_view_id: 3, bucket_id: 4, position: 65536},
			{project_view_id: 5},
			{bucket_id: 6},
			'junk',
		],
	})).toEqual({
		kind: 'task.updated',
		taskId: 9,
		projectId: 1,
		oldProjectId: 2,
		placements: [
			{project_view_id: 3, bucket_id: 4, position: 65536},
			{project_view_id: 5},
		],
	})
	expect(parseProjectTaskEvent('task.deleted', {project_id: 1, task_id: 9})).toEqual({
		kind: 'task.deleted',
		taskId: 9,
		projectId: 1,
		placements: [],
	})
	expect(parseProjectTaskEvent('timer.created', {project_id: 1, task_id: 9})).toBeNull()
	expect(parseProjectTaskEvent(undefined, {project_id: 1, task_id: 9})).toBeNull()
	expect(parseProjectTaskEvent('task.updated', null)).toBeNull()
	expect(parseProjectTaskEvent('task.updated', {project_id: 1})).toBeNull()
	expect(parseProjectTaskEvent('task.updated', {project_id: 0, task_id: 9})).toBeNull()
})

function projectBoard(client: QueryClient, project: number, view: number, tasks: {id: number, position: number}[], count = tasks.length) {
	const key = kanbanKeys.board(project, view)
	client.setQueryData(projectKeys.detail(project), {
		id: project,
		views: [{
			id: view,
			bucket_configuration_mode: 'manual',
			default_bucket_id: 3,
		}],
	})
	client.setQueryData(key, {
		buckets: [
			normalizeBucket({id: 3, count, tasks: tasks.map(task => ({...task, project_id: project, bucket_id: 3, title: 'old'}))}),
			normalizeBucket({id: 4, count: 0, tasks: []}),
		],
		pages: {3: 1, 4: 1},
	})
	return key
}

const boardShape = (client: QueryClient, key: ReturnType<typeof kanbanKeys.board>) =>
	client.getQueryData<BoardData>(key)?.buckets.map(bucket => ({
		count: bucket.count,
		tasks: bucket.tasks.map(task => [task.id, task.title, task.position]),
	}))

function liveEvent(event: Partial<ProjectTaskEvent> & Pick<ProjectTaskEvent, 'kind'>): ProjectTaskEvent {
	return {
		taskId: 9,
		projectId: 1,
		placements: [],
		...event,
	}
}

it('loads an updated task from the API and moves its card to the stored bucket', async () => {
	const client = new QueryClient()
	const board = projectBoard(client, 1, 2, [{id: 9, position: 100}, {id: 1, position: 200}])
	const list = taskKeys.list({project: 1})
	client.setQueryData(list, {items: [normalizeTask({id: 9, title: 'old', project_id: 1})], total: 1, page: 1, per_page: 25, total_pages: 1})
	client.setQueryData(taskKeys.detail(9), normalizeTask({id: 9, title: 'old', project_id: 1}))
	sdk.tasksRead.mockResolvedValue({data: {id: 9, project_id: 1, title: 'new', done: true}})

	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(liveEvent({
		kind: 'task.updated',
		placements: [{project_view_id: 2, bucket_id: 4, position: 50}],
	}))

	expect(sdk.tasksRead).toHaveBeenCalledWith(expect.objectContaining({path: {task: 9}}))
	expect(boardShape(client, board)).toEqual([
		{count: 1, tasks: [[1, 'old', 200]]},
		{count: 1, tasks: [[9, 'new', 50]]},
	])
	expect(client.getQueryData<TaskResponse>(taskKeys.detail(9))?.title).toBe('new')
	expect(client.getQueryState(list)?.isInvalidated).toBe(true)
	expect(client.getQueryState(board)?.isInvalidated).toBe(true)
	expect(client.getQueryState(board)?.fetchStatus).toBe('idle')
})

it('inserts a created task by position and counts it', async () => {
	const client = new QueryClient()
	const board = projectBoard(client, 1, 2, [{id: 1, position: 100}, {id: 2, position: 300}])
	sdk.tasksRead.mockResolvedValue({data: {id: 9, project_id: 1, title: 'new'}})

	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(liveEvent({
		kind: 'task.created',
		placements: [{project_view_id: 2, bucket_id: 3, position: 200}],
	}))

	expect(boardShape(client, board)).toEqual([
		{count: 3, tasks: [[1, 'old', 100], [9, 'new', 200], [2, 'old', 300]]},
		{count: 0, tasks: []},
	])
})

it('drops a deleted task without asking the API', async () => {
	const client = new QueryClient()
	const board = projectBoard(client, 1, 2, [{id: 9, position: 100}, {id: 1, position: 200}])
	client.setQueryData(taskKeys.detail(9), normalizeTask({id: 9, project_id: 1}))
	sdk.tasksRead.mockReset()

	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(liveEvent({kind: 'task.deleted'}))

	expect(sdk.tasksRead).not.toHaveBeenCalled()
	expect(boardShape(client, board)).toEqual([
		{count: 1, tasks: [[1, 'old', 200]]},
		{count: 0, tasks: []},
	])
	expect(client.getQueryData(taskKeys.detail(9))).toBeUndefined()
})

it.each([403, 404])('drops a task the API answers %d for', async status => {
	const client = new QueryClient()
	const board = projectBoard(client, 1, 2, [{id: 9, position: 100}])
	sdk.tasksRead.mockRejectedValue(new ApiError({status, detail: 'gone'}))

	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(liveEvent({kind: 'task.updated'}))

	expect(boardShape(client, board)).toEqual([
		{count: 0, tasks: []},
		{count: 0, tasks: []},
	])
})

it('keeps the cache when loading the task fails for another reason', async () => {
	const client = new QueryClient()
	const board = projectBoard(client, 1, 2, [{id: 9, position: 100}])
	sdk.tasksRead.mockRejectedValue(new ApiError({status: 500, detail: 'boom'}))

	await expect(client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(liveEvent({kind: 'task.updated'})))
		.rejects.toThrow('boom')

	expect(boardShape(client, board)?.[0].tasks).toEqual([[9, 'old', 100]])
})

it('removes a moved task from the previous project and places it in the new one', async () => {
	const client = new QueryClient()
	const oldBoard = projectBoard(client, 2, 5, [{id: 9, position: 100}])
	const newBoard = projectBoard(client, 1, 2, [{id: 1, position: 100}])
	sdk.tasksRead.mockResolvedValue({data: {id: 9, project_id: 1, title: 'moved'}})

	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute(liveEvent({
		kind: 'task.updated',
		oldProjectId: 2,
		placements: [{project_view_id: 2, bucket_id: 4, position: 50}],
	}))

	expect(boardShape(client, oldBoard)?.[0]).toEqual({count: 0, tasks: []})
	expect(boardShape(client, newBoard)).toEqual([
		{count: 1, tasks: [[1, 'old', 100]]},
		{count: 1, tasks: [[9, 'moved', 50]]},
	])
})

it('refetches open boards and lists on a reconnect', async () => {
	const client = new QueryClient()
	const boardFn = vi.fn(() => ({buckets: [], pages: {}}))
	const unsubscribe = new QueryObserver(client, {
		queryKey: kanbanKeys.board(1, 2),
		queryFn: boardFn,
	}).subscribe(() => {})
	await vi.waitFor(() => expect(boardFn).toHaveBeenCalledTimes(1))

	await client.getMutationCache().build(client, serverCacheEventMutationOptions()).execute({kind: 'reconnect'})

	await vi.waitFor(() => expect(boardFn).toHaveBeenCalledTimes(2))
	unsubscribe()
})
