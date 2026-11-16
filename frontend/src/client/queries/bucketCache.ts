import type {QueryClient, QueryKey} from '@tanstack/vue-query'
import {kanbanKeys, normalizeBucket, type BoardData, type BucketResponse} from './kanban'
import {insertByPosition} from './taskCache'

// The bucket fields a bucket event carries; tasks and count stay what the board already holds.
export type LiveBucket = {
	id: number,
	project_view_id: number,
	title: string,
	limit: number,
	position: number,
}

const byPosition = (a: BucketResponse, b: BucketResponse) => a.position - b.position

// Applies a bucket created or changed elsewhere to every cached board of its view. A bucket that is
// already there (the doer's own tab) is merged rather than added twice.
export function upsertBucketInBoards(client: QueryClient, project: number, view: number, bucket: LiveBucket) {
	client.setQueriesData<BoardData>({queryKey: kanbanKeys.view(project, view)}, current => {
		if (!current) return current
		const existing = current.buckets.find(item => item.id === bucket.id)
		const next = existing
			? {...existing, title: bucket.title, limit: bucket.limit, position: bucket.position}
			: normalizeBucket({...bucket, count: 0, tasks: []})
		return {
			...current,
			buckets: [...current.buckets.filter(item => item.id !== bucket.id), next].sort(byPosition),
			pages: existing ? current.pages : {...current.pages, [bucket.id]: 1},
		}
	})
}

// Drops a bucket deleted elsewhere from every cached board of its view and hands its cards to the
// bucket the server moved them to, by position. Returns the boards that could not be patched
// because that bucket is unknown while the removed one held tasks: they need a refetch.
export function removeBucketFromBoards(
	client: QueryClient,
	project: number,
	view: number,
	bucketId: number,
	movedTo: number | undefined,
): QueryKey[] {
	const stale: QueryKey[] = []
	for (const [key, board] of client.getQueriesData<BoardData>({queryKey: kanbanKeys.view(project, view)})) {
		const removed = board?.buckets.find(item => item.id === bucketId)
		if (!board || !removed) continue
		const target = movedTo === bucketId ? undefined : board.buckets.find(item => item.id === movedTo)
		if (!target && removed.count > 0) stale.push(key)
		let merged: BucketResponse | undefined
		if (target) {
			// The target's own count decides whether a card past its loaded page waits for a later page.
			let tasks = target.tasks
			for (const card of removed.tasks) {
				tasks = insertByPosition({...target, tasks}, {...card, bucket_id: target.id}, false) ?? tasks
			}
			merged = {...target, tasks, count: target.count + removed.count}
		}
		const {[bucketId]: _page, ...pages} = board.pages
		client.setQueryData<BoardData>(key, {
			...board,
			buckets: board.buckets
				.filter(item => item.id !== bucketId)
				.map(item => item.id === merged?.id ? merged : item),
			pages,
		})
	}
	return stale
}
