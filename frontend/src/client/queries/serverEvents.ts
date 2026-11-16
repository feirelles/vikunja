import {
	useMutation,
	type QueryClient,
	type QueryKey,
} from '@tanstack/vue-query'
import {tasksRead, type TimeEntry} from '@/client/generated'
import {ApiError} from '@/client/problem'
import {contextMutationOptions} from './contextMutation'
import {
	cachedTaskIdOf,
	normalizeTimeEntry,
	patchTimeEntry,
	removeTimeEntry,
	timeEntryKeys,
	type TimeEntryResponse,
} from './timeEntries'
import {notificationKeys} from './notifications'
import {commentKeys} from './comments'
import {
	normalizeTask,
	taskKeys,
	type TaskResponse,
} from './tasks'
import {
	BUCKET_TASK_PARAMS,
	bucketKeys,
	kanbanKeys,
} from './kanban'
import {
	invalidateTaskMembership,
	mapTaskEverywhere,
	placeTaskInBoards,
	removeTaskEverywhere,
	replaceTaskEverywhere,
	taskQueryKeys,
	type TaskPlacement,
} from './taskCache'
import {
	removeBucketFromBoards,
	upsertBucketInBoards,
	type LiveBucket,
} from './bucketCache'
import {getCachedProject, projectKeys} from './projects'

export const CACHE_EVENTS = [
	'timer.created',
	'timer.updated',
	'timer.deleted',
	'notification.created',
] as const

export type ServerCacheEventName = typeof CACHE_EVENTS[number]

// Pushed to the subscribers of a project; the payload carries ids and placements only.
export const PROJECT_TASK_EVENTS = [
	'task.created',
	'task.updated',
	'task.deleted',
	'task.restored',
] as const

export type ProjectTaskEventName = typeof PROJECT_TASK_EVENTS[number]

export type ProjectTaskEvent = {
	kind: ProjectTaskEventName,
	taskId: number,
	projectId: number,
	oldProjectId?: number,
	placements: TaskPlacement[],
}

export type ProjectCommentEvent = {
	kind: 'task.comment.created' | 'task.comment.edited' | 'task.comment.deleted',
	taskId: number,
	projectId: number,
	commentId: number,
}

// Bucket frames carry the bucket itself: it has no per-user fields.
export type ProjectBucketEvent = {
	kind: 'bucket.created' | 'bucket.updated',
	projectId: number,
	viewId: number,
	bucket: LiveBucket,
} | {
	kind: 'bucket.deleted',
	projectId: number,
	viewId: number,
	bucketId: number,
	movedToBucketId?: number,
}

// Every stored position of the view changed.
export type PositionsRecalculatedEvent = {
	kind: 'task.positions.recalculated',
	projectId: number,
	viewId: number,
}

export type ProjectEvent = ProjectTaskEvent | ProjectCommentEvent | ProjectBucketEvent | PositionsRecalculatedEvent

type TimerEvent = 'timer.created' | 'timer.updated' | 'timer.deleted'
export type ServerCacheEvent =
	| {
		kind: TimerEvent,
		entry: TimeEntryResponse,
	}
	| ProjectEvent
	| {
		kind: 'notifications',
		taskId?: number,
	}
	| {
		kind: 'subscribed',
		since: number,
	}
	| {kind: 'reconnect'}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === 'object' && value !== null
}

export function parseServerCacheEvent(
	event: ServerCacheEventName,
	data: unknown,
	currentUserId: number | undefined,
): ServerCacheEvent | null {
	if (!isRecord(data)) return null
	if (event === 'timer.created' || event === 'timer.updated' || event === 'timer.deleted') {
		if (currentUserId === undefined || !isRecord(data.user) || data.user.id !== currentUserId) return null
		if (typeof data.id !== 'number' || data.id <= 0) return null
		if (data.task_id !== undefined && data.task_id !== null && typeof data.task_id !== 'number') return null
		return {
			kind: event,
			entry: normalizeTimeEntry(data as TimeEntry),
		}
	}
	if (event === 'notification.created') {
		const task = data.name === 'task.comment' && isRecord(data.notification) ? data.notification.task : undefined
		const taskId = isRecord(task) && typeof task.id === 'number' && task.id > 0 ? task.id : undefined
		if (taskId === undefined && !(typeof data.id === 'number' && data.id > 0)) return null
		return {
			kind: 'notifications',
			taskId,
		}
	}
	return null
}

function isProjectTaskEventName(event: string): event is ProjectTaskEventName {
	return (PROJECT_TASK_EVENTS as readonly string[]).includes(event)
}

function parsePlacements(value: unknown): TaskPlacement[] {
	if (!Array.isArray(value)) return []
	const placements: TaskPlacement[] = []
	for (const item of value) {
		if (!isRecord(item) || typeof item.project_view_id !== 'number') continue
		placements.push({
			project_view_id: item.project_view_id,
			...(typeof item.bucket_id === 'number' ? {bucket_id: item.bucket_id} : {}),
			...(typeof item.position === 'number' ? {position: item.position} : {}),
		})
	}
	return placements
}

export function parseProjectTaskEvent(event: string | undefined, data: unknown): ProjectTaskEvent | null {
	if (event === undefined || !isProjectTaskEventName(event) || !isRecord(data)) return null
	if (typeof data.task_id !== 'number' || data.task_id <= 0) return null
	if (typeof data.project_id !== 'number' || data.project_id <= 0) return null
	return {
		kind: event,
		taskId: data.task_id,
		projectId: data.project_id,
		...(typeof data.old_project_id === 'number' && data.old_project_id > 0 ? {oldProjectId: data.old_project_id} : {}),
		placements: parsePlacements(data.placements),
	}
}

const positiveId = (value: unknown): value is number => typeof value === 'number' && value > 0

function parseProjectCommentEvent(event: string, data: Record<string, unknown>): ProjectCommentEvent | null {
	if (event !== 'task.comment.created' && event !== 'task.comment.edited' && event !== 'task.comment.deleted') return null
	if (!positiveId(data.task_id) || !positiveId(data.project_id) || !positiveId(data.comment_id)) return null
	return {
		kind: event,
		taskId: data.task_id,
		projectId: data.project_id,
		commentId: data.comment_id,
	}
}

function parseLiveBucket(value: unknown, id: number, viewId: number): LiveBucket | null {
	if (!isRecord(value) || value.id !== id) return null
	if (typeof value.title !== 'string' || typeof value.limit !== 'number' || typeof value.position !== 'number') return null
	return {
		id,
		project_view_id: viewId,
		title: value.title,
		limit: value.limit,
		position: value.position,
	}
}

function parseProjectBucketEvent(event: string, data: Record<string, unknown>): ProjectBucketEvent | null {
	if (event !== 'bucket.created' && event !== 'bucket.updated' && event !== 'bucket.deleted') return null
	if (!positiveId(data.project_id) || !positiveId(data.project_view_id) || !positiveId(data.bucket_id)) return null
	const scope = {projectId: data.project_id, viewId: data.project_view_id}
	if (event === 'bucket.deleted') {
		return {
			kind: event,
			...scope,
			bucketId: data.bucket_id,
			...(positiveId(data.moved_to_bucket_id) ? {movedToBucketId: data.moved_to_bucket_id} : {}),
		}
	}
	const bucket = parseLiveBucket(data.bucket, data.bucket_id, data.project_view_id)
	return bucket ? {kind: event, ...scope, bucket} : null
}

// Every frame pushed to the subscribers of a project, or null for one this client does not handle.
export function parseProjectEvent(event: string | undefined, data: unknown): ProjectEvent | null {
	if (event === undefined || !isRecord(data)) return null
	if (isProjectTaskEventName(event)) return parseProjectTaskEvent(event, data)
	if (event === 'task.positions.recalculated') {
		if (!positiveId(data.project_id) || !positiveId(data.project_view_id)) return null
		return {kind: event, projectId: data.project_id, viewId: data.project_view_id}
	}
	return parseProjectCommentEvent(event, data) ?? parseProjectBucketEvent(event, data)
}

function isProjectTaskEvent(event: ServerCacheEvent): event is ProjectTaskEvent {
	return 'taskId' in event && 'placements' in event
}

function isProjectCommentEvent(event: ServerCacheEvent): event is ProjectCommentEvent {
	return 'commentId' in event
}

function isProjectBucketEvent(event: ServerCacheEvent): event is ProjectBucketEvent {
	return event.kind === 'bucket.created' || event.kind === 'bucket.updated' || event.kind === 'bucket.deleted'
}

function applyProjectBucketEvent(client: QueryClient, event: ProjectBucketEvent): Promise<unknown> {
	const {projectId, viewId} = event
	const settled: Promise<unknown>[] = [client.invalidateQueries({queryKey: bucketKeys.list(projectId, viewId)})]
	if (event.kind !== 'bucket.deleted') {
		upsertBucketInBoards(client, projectId, viewId, event.bucket)
		return Promise.all(settled)
	}
	for (const queryKey of removeBucketFromBoards(client, projectId, viewId, event.bucketId, event.movedToBucketId)) {
		settled.push(client.invalidateQueries({queryKey, exact: true, refetchType: 'active'}))
	}
	// The server clears the view's default and done bucket when it deletes them.
	const view = getCachedProject(projectId, client)?.views?.find(item => item.id === viewId)
	if (view && (view.default_bucket_id === event.bucketId || view.done_bucket_id === event.bucketId)) {
		settled.push(client.invalidateQueries({queryKey: projectKeys.all}))
	}
	return Promise.all(settled)
}

// Only the counters of the cards that show the task change; the comments themselves are refetched.
function applyCommentCounters(client: QueryClient, taskId: number, task: TaskResponse) {
	mapTaskEverywhere(client, taskId, current => ({
		...current,
		...(task.comment_count === undefined ? {} : {comment_count: task.comment_count}),
		...(task.is_unread === undefined ? {} : {is_unread: task.is_unread}),
	}))
}

// The event names the task only; the task itself comes through the REST API with the reader's
// permissions. Gone (deleted meanwhile) or forbidden (not shared with the reader) both drop it.
async function fetchLiveTask(id: number): Promise<TaskResponse | 'gone'> {
	try {
		return normalizeTask((await tasksRead({
			path: {task: id},
			query: {expand: BUCKET_TASK_PARAMS.expand},
		})).data)
	} catch (cause) {
		if (cause instanceof ApiError && (cause.status === 403 || cause.status === 404)) return 'gone'
		throw cause
	}
}

function applyProjectTaskEvent(client: QueryClient, event: ProjectTaskEvent, task: TaskResponse | 'gone' | undefined) {
	if (event.kind === 'task.deleted' || task === 'gone') {
		removeTaskEverywhere(client, event.taskId)
		return
	}
	if (task === undefined) return
	// Boards are patched rather than refetched: a refetch would drop every loaded page but the first.
	replaceTaskEverywhere(client, task)
	placeTaskInBoards(client, task, event.placements, event.kind !== 'task.updated' || event.oldProjectId !== undefined)
}

const startsWith = (key: QueryKey, prefix: readonly unknown[]) => prefix.every((part, index) => key[index] === part)
const isTaskDetailKey = (key: QueryKey) => startsWith(key, taskKeys.details)

const SUBSCRIBE_SWEEP_KEYS = [
	notificationKeys.all,
	timeEntryKeys.all,
	commentKeys.all,
	taskKeys.details,
]

// Only collections that already hold the task can have gone stale; its own detail key is invalidated anyway.
async function invalidateCachedTask(client: QueryClient, taskId: number | undefined): Promise<void> {
	if (!taskId) return
	const heldByCollection = taskQueryKeys(client, taskId).some(key => !isTaskDetailKey(key))
	await Promise.all([
		client.invalidateQueries({queryKey: taskKeys.detail(taskId)}),
		...(heldByCollection ? [invalidateTaskMembership(client)] : []),
	])
}

export function serverCacheEventMutationOptions() {
	return contextMutationOptions({
		mutationFn: async (event: ServerCacheEvent, {client}): Promise<TaskResponse | 'gone' | undefined> => {
			// A comment only matters to the counters of a task this client shows.
			if (isProjectCommentEvent(event)) {
				return taskQueryKeys(client, event.taskId).length > 0 ? fetchLiveTask(event.taskId) : undefined
			}
			if (!isProjectTaskEvent(event) || event.kind === 'task.deleted') return undefined
			return fetchLiveTask(event.taskId)
		},
		onSuccess: (task, event, client) => {
			if (isProjectTaskEvent(event)) applyProjectTaskEvent(client, event, task)
			// A task gone meanwhile is dropped by its own task.deleted frame.
			if (isProjectCommentEvent(event) && task !== undefined && task !== 'gone') applyCommentCounters(client, event.taskId, task)
		},
		onSettled: (event, client) => {
			if (isProjectCommentEvent(event)) {
				return client.invalidateQueries({queryKey: commentKeys.task(event.taskId)})
			}
			if (isProjectBucketEvent(event)) {
				return applyProjectBucketEvent(client, event)
			}
			if (event.kind === 'task.positions.recalculated') {
				// Cached positions are all stale, and a drag between two of them would store a wrong one.
				return Promise.all([
					client.invalidateQueries({queryKey: kanbanKeys.view(event.projectId, event.viewId), refetchType: 'active'}),
					client.invalidateQueries({queryKey: taskKeys.lists}),
					client.invalidateQueries({queryKey: taskKeys.allLists}),
				])
			}
			if (isProjectTaskEvent(event)) {
				// Lists are sorted and filtered by the server, so membership is theirs to decide.
				return invalidateTaskMembership(client, event.taskId, 'active')
			}
			if ('entry' in event) {
				// The payload carries only the new task, so a reassignment would leave the old one's count stale.
				const previousTaskId = event.kind === 'timer.updated' ? cachedTaskIdOf(client, event.entry.id) : undefined
				if (event.kind === 'timer.deleted') removeTimeEntry(client, event.entry.id)
				else patchTimeEntry(client, event.entry)
				return Promise.all([
					// Whether a new entry belongs to a server-filtered list cannot be decided from the payload.
					...(event.kind === 'timer.created' ? [client.invalidateQueries({queryKey: timeEntryKeys.lists})] : []),
					invalidateCachedTask(client, event.entry.task_id),
					...(previousTaskId === event.entry.task_id ? [] : [invalidateCachedTask(client, previousTaskId)]),
				])
			}
			if (event.kind === 'notifications') {
				const taskId = event.taskId
				return Promise.all([
					client.invalidateQueries({queryKey: notificationKeys.all}),
					...(taskId === undefined ? [] : [
						client.invalidateQueries({queryKey: commentKeys.task(taskId)}),
						invalidateCachedTask(client, taskId),
					]),
				])
			}
			if (event.kind === 'subscribed') {
				// Data that arrived before the subscribe frames went out may have missed an event nobody saw.
				return client.invalidateQueries({
					predicate: query => query.state.dataUpdatedAt > 0
						&& query.state.dataUpdatedAt < event.since
						&& SUBSCRIBE_SWEEP_KEYS.some(prefix => startsWith(query.queryKey, prefix)),
				})
			}
			// A dropped connection may have missed task events, so the open collections reload too.
			return Promise.all([
				client.invalidateQueries({queryKey: timeEntryKeys.all}),
				client.invalidateQueries({queryKey: notificationKeys.all}),
				client.invalidateQueries({queryKey: commentKeys.all}),
				client.invalidateQueries({queryKey: taskKeys.details}),
				invalidateTaskMembership(client, undefined, 'active'),
				client.invalidateQueries({queryKey: kanbanKeys.all, refetchType: 'active'}),
				client.invalidateQueries({queryKey: bucketKeys.all}),
			])
		},
		toastError: () => false,
	})
}

export function useServerCacheEventMutation() {
	return useMutation(serverCacheEventMutationOptions())
}
