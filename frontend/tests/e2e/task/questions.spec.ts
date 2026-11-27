import {test, expect} from '../../support/fixtures'
import {ProjectFactory} from '../../factories/project'
import {TaskFactory} from '../../factories/task'
import {BucketFactory} from '../../factories/bucket'
import {TaskBucketFactory} from '../../factories/task_buckets'
import {createDefaultViews} from '../project/prepareProjects'

// The fixture's request context points at /api/v1/; questions only exist on v2.
const v2 = (path: string) => `../v2/${path}`

test('a question asked over the API is answered in the task detail', async ({authenticatedPage: page, apiContext, userToken}, testInfo) => {
	const headers = {Authorization: `Bearer ${userToken}`}
	const [project] = await ProjectFactory.create(1)
	const views = await createDefaultViews(project.id)
	const kanban = views[3]
	const [bucket] = await BucketFactory.create(1, {project_view_id: kanban.id, title: 'Doing'})
	const [task] = await TaskFactory.create(1, {id: 1, project_id: project.id, title: 'Pick a storage backend'})
	await TaskBucketFactory.create(1, {task_id: task.id, bucket_id: bucket.id, project_view_id: kanban.id})

	const ask = async (data: object) => {
		const res = await apiContext.post(v2(`tasks/${task.id}/questions`), {headers, data})
		expect(res.ok(), await res.text()).toBeTruthy()
		return (await res.json()).id as number
	}
	const storage = await ask({
		title: 'Where should uploads go?',
		summary: 'Uploads are on the local disk today.',
		details: '<p>A host rebuild loses the local disk.</p>',
		options: [
			{key: 'A', label: 'Local disk'},
			{key: 'B', label: 'S3', description: 'Needs credentials'},
			{key: 'C', label: 'Both'},
		],
		recommendation: {key: 'B', reason: 'Survives a host rebuild'},
	})
	await ask({
		title: 'Keep the old endpoint?',
		options: [{key: 'A', label: 'Yes'}, {key: 'B', label: 'No'}],
	})
	const answeredId = await ask({
		title: 'Name of the bucket?',
		options: [{key: 'A', label: 'uploads'}, {key: 'B', label: 'files'}],
	})
	const answered = await apiContext.post(v2(`tasks/${task.id}/questions/${answeredId}/answer`), {headers, data: {option: 'A', notes: 'short and clear'}})
	expect(answered.ok()).toBeTruthy()

	await page.goto(`/projects/${project.id}/${kanban.id}`)
	const card = page.locator('.kanban .bucket .task').filter({hasText: task.title})
	await expect(card.locator('.question-count')).toContainText('2 questions')
	await page.setViewportSize({width: 1400, height: 900})
	await page.screenshot({path: testInfo.outputPath('kanban-1400.png')})

	await page.goto(`/tasks/${task.id}`)
	const questions = page.locator('.task-questions')
	await expect(questions.locator('.task-question')).toHaveCount(3)
	await expect(questions.locator('.question-option.is-recommended')).toContainText('S3')
	await expect(questions.locator('.answered-line')).toContainText('uploads')
	await page.screenshot({path: testInfo.outputPath('detail-1400.png'), fullPage: true})
	await page.setViewportSize({width: 400, height: 900})
	await page.screenshot({path: testInfo.outputPath('detail-400.png'), fullPage: true})
	await page.setViewportSize({width: 1400, height: 900})

	const first = questions.locator('.task-question').filter({hasText: 'Where should uploads go?'})
	await first.locator('textarea').fill('S3 bucket in the homelab MinIO')
	const answer = page.waitForResponse(r => r.url().endsWith(`/questions/${storage}/answer`) && r.request().method() === 'POST')
	await first.locator('.question-option').filter({hasText: 'S3'}).click()
	expect((await answer).ok()).toBeTruthy()

	await expect(questions.locator('.answered-line')).toHaveCount(2)
	const stored = await apiContext.get(v2(`tasks/${task.id}/questions/${storage}`), {headers})
	const body = await stored.json()
	expect(body.status).toBe('answered')
	expect(body.answer_option).toBe('B')
	expect(body.answer_notes).toBe('S3 bucket in the homelab MinIO')

	const filtered = await apiContext.get(v2('tasks'), {headers, params: {filter: 'questions = open'}})
	expect((await filtered.json()).items.map((t: {id: number}) => t.id)).toEqual([task.id])
	await page.screenshot({path: testInfo.outputPath('detail-after-answer-1400.png'), fullPage: true})
})
