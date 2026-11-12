import type {Page} from '@playwright/test'
import {
	test,
	expect,
} from '../../support/fixtures'
import {ProjectFactory} from '../../factories/project'
import {ProjectViewFactory} from '../../factories/project_view'
import {BucketFactory} from '../../factories/bucket'
import {TaskFactory} from '../../factories/task'
import {TaskBucketFactory} from '../../factories/task_buckets'

function waitForProjectSubscription(page: Page, projectId: number) {
	return page.waitForEvent('websocket').then(socket => socket.waitForEvent('framesent', {
		predicate: frame => {
			const message = JSON.parse(frame.payload.toString())
			return message.action === 'subscribe' && message.event === 'project' && message.project_id === projectId
		},
	}))
}

test('a task changed elsewhere updates the open kanban board without a reload', async ({
	authenticatedPage: page,
	apiContext,
	userToken,
}) => {
	const [project] = await ProjectFactory.create(1)
	const [view] = await ProjectViewFactory.create(1, {
		id: 1,
		project_id: project.id,
		view_kind: 3,
		bucket_configuration_mode: 1,
	})
	const buckets = await BucketFactory.create(2, {project_view_id: view.id})
	const [task] = await TaskFactory.create(1, {
		project_id: project.id,
		title: 'Before the change',
		index: 1,
	})
	await TaskBucketFactory.create(1, {
		task_id: task.id,
		bucket_id: buckets[0].id,
		project_view_id: view.id,
	})

	const subscribed = waitForProjectSubscription(page, project.id)
	await page.goto(`/projects/${project.id}/${view.id}`)
	await subscribed
	const cardsIn = (bucket: number) => page.locator('.kanban .bucket').nth(bucket).locator('.tasks .task')
	await expect(cardsIn(0)).toHaveCount(1)
	const headers = {Authorization: `Bearer ${userToken}`}

	const renamed = await apiContext.patch(`/api/v2/tasks/${task.id}`, {
		headers,
		data: {title: 'After the change'},
	})
	expect(renamed.ok()).toBeTruthy()
	await expect(cardsIn(0).filter({hasText: 'After the change'})).toBeVisible()

	const moved = await apiContext.put(`/api/v2/projects/${project.id}/views/${view.id}/buckets/${buckets[1].id}/tasks`, {
		headers,
		data: {task_id: task.id},
	})
	expect(moved.ok()).toBeTruthy()
	await expect(cardsIn(1).filter({hasText: 'After the change'})).toBeVisible()
	await expect(cardsIn(0)).toHaveCount(0)

	const created = await apiContext.post(`/api/v2/projects/${project.id}/tasks`, {
		headers,
		data: {title: 'Created elsewhere'},
	})
	expect(created.ok()).toBeTruthy()
	await expect(cardsIn(0).filter({hasText: 'Created elsewhere'})).toBeVisible()

	const deleted = await apiContext.delete(`/api/v2/tasks/${task.id}`, {headers})
	expect(deleted.ok()).toBeTruthy()
	await expect(cardsIn(1)).toHaveCount(0)

	// The patched board matches what the server holds.
	await page.reload()
	await expect(cardsIn(0).filter({hasText: 'Created elsewhere'})).toBeVisible()
	await expect(cardsIn(1)).toHaveCount(0)
})
