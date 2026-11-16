import {
	onScopeDispose,
	toValue,
	watch,
	type MaybeRefOrGetter,
} from 'vue'
import {useWebSocket} from './useWebSocket'
import {
	parseProjectEvent,
	useServerCacheEventMutation,
} from '@/client/queries/serverEvents'

// Keeps the cached tasks, comments and buckets of a project in step with changes made elsewhere while one of its views is
// open. Pseudo projects (favorites, saved filters) have no subscription: their membership is decided
// per request, so their lists keep relying on refetches.
export function useProjectLiveUpdates(projectId: MaybeRefOrGetter<number>) {
	const socket = useWebSocket()
	const mutation = useServerCacheEventMutation()
	let unsubscribe: (() => void) | undefined

	watch(() => toValue(projectId), id => {
		unsubscribe?.()
		unsubscribe = undefined
		if (!(id > 0)) return
		unsubscribe = socket.subscribeProject(id, message => {
			const event = parseProjectEvent(message.event, message.data)
			if (event) mutation.mutate(event)
		})
	}, {immediate: true})

	onScopeDispose(() => unsubscribe?.())
}
