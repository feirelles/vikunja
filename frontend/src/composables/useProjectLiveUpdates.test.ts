import {
	afterEach,
	beforeEach,
	expect,
	it,
	vi,
} from 'vitest'
import {
	defineComponent,
	ref,
} from 'vue'
import {
	flushPromises,
	mount,
} from '@vue/test-utils'
import {
	QueryClient,
	VueQueryPlugin,
} from '@tanstack/vue-query'
import {useProjectLiveUpdates} from './useProjectLiveUpdates'
import {
	normalizeTask,
	taskKeys,
} from '@/client/queries/tasks'

const ws = vi.hoisted(() => ({
	handlers: new Map<number, (message: {event?: string, data: unknown}) => void>(),
	subscribed: [] as number[],
	unsubscribed: [] as number[],
}))

vi.mock('./useWebSocket', () => ({
	useWebSocket: () => ({
		subscribeProject: (projectId: number, callback: (message: {event?: string, data: unknown}) => void) => {
			ws.handlers.set(projectId, callback)
			ws.subscribed.push(projectId)
			return () => {
				ws.handlers.delete(projectId)
				ws.unsubscribed.push(projectId)
			}
		},
	}),
}))
vi.mock('@/message', () => ({
	error: vi.fn(),
	success: vi.fn(),
}))

let client: QueryClient

beforeEach(() => {
	ws.handlers.clear()
	ws.subscribed = []
	ws.unsubscribed = []
	client = new QueryClient()
})

afterEach(() => {
	client.clear()
})

function mountLiveUpdates(projectId = ref(5)) {
	const wrapper = mount(defineComponent({
		setup() {
			useProjectLiveUpdates(projectId)
			return () => null
		},
	}), {global: {plugins: [[VueQueryPlugin, {queryClient: client}]]}})
	return {wrapper, projectId}
}

it('subscribes to the project while mounted and follows the project id', async () => {
	const {wrapper, projectId} = mountLiveUpdates()
	expect(ws.subscribed).toEqual([5])

	projectId.value = 6
	await flushPromises()
	expect(ws.unsubscribed).toEqual([5])
	expect(ws.subscribed).toEqual([5, 6])

	wrapper.unmount()
	expect(ws.unsubscribed).toEqual([5, 6])
})

it('does not subscribe to pseudo projects', async () => {
	const {projectId} = mountLiveUpdates(ref(-1))
	expect(ws.subscribed).toEqual([])

	projectId.value = 5
	await flushPromises()
	expect(ws.subscribed).toEqual([5])

	projectId.value = -2
	await flushPromises()
	expect(ws.unsubscribed).toEqual([5])
	expect(ws.handlers.size).toBe(0)
})

it('applies a task frame of the project to the cache', async () => {
	const list = taskKeys.list({project: 5})
	client.setQueryData(list, {items: [normalizeTask({id: 9, project_id: 5})], total: 1, page: 1, per_page: 25, total_pages: 1})
	mountLiveUpdates()

	ws.handlers.get(5)!({event: 'task.deleted', data: {project_id: 5, task_id: 9}})
	await flushPromises()

	expect(client.getQueryData<{items: unknown[]}>(list)?.items).toEqual([])
	expect(client.getMutationCache().getAll()).toHaveLength(1)
})

it('ignores frames it cannot parse', async () => {
	mountLiveUpdates()

	ws.handlers.get(5)!({event: 'task.updated', data: {project_id: 5}})
	ws.handlers.get(5)!({event: 'timer.created', data: {project_id: 5, task_id: 9}})
	await flushPromises()

	expect(client.getMutationCache().getAll()).toHaveLength(0)
})
