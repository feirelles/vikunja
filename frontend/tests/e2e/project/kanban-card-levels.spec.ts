import {test, expect} from '../../support/fixtures'
import {ProjectFactory} from '../../factories/project'
import {TaskFactory} from '../../factories/task'
import {BucketFactory} from '../../factories/bucket'
import {TaskBucketFactory} from '../../factories/task_buckets'
import {TaskRelationFactory} from '../../factories/task_relation'
import {createDefaultViews} from './prepareProjects'

// An objective in project 1 with three subtasks: two in project 1, one in project 2.
async function seedObjective() {
	const projects = await ProjectFactory.create(2, {title: (i: number) => i === 1 ? 'Objectives' : 'Elsewhere'})
	const views = await createDefaultViews(projects[0].id)
	await createDefaultViews(projects[1].id, 5)
	const kanban = views[3]
	const [bucket] = await BucketFactory.create(1, {project_view_id: kanban.id, title: 'Doing'})

	const tasks = await TaskFactory.create(4, {
		// Factory callbacks count from 1.
		id: (i: number) => i,
		index: (i: number) => i,
		project_id: (i: number) => i === 4 ? projects[1].id : projects[0].id,
		title: (i: number) => ['Ship the release', 'Write the notes', 'Tag the build', 'Update the docs'][i - 1],
		done: (i: number) => i === 2,
	})
	for (const task of tasks.slice(0, 3)) {
		await TaskBucketFactory.create(1, {task_id: task.id, bucket_id: bucket.id, project_view_id: kanban.id}, false)
	}
	let relation = 1
	for (const child of tasks.slice(1)) {
		await TaskRelationFactory.create(1, {id: relation++, task_id: tasks[0].id, other_task_id: child.id, relation_kind: 'subtask'}, relation === 2)
		await TaskRelationFactory.create(1, {id: relation++, task_id: child.id, other_task_id: tasks[0].id, relation_kind: 'parenttask'}, false)
	}
	return {project: projects[0], kanban}
}

test('the kanban hides subtasks and shows their progress on the parent', async ({authenticatedPage: page}, testInfo) => {
	const {project, kanban} = await seedObjective()
	await page.setViewportSize({width: 1400, height: 900})
	await page.goto(`/projects/${project.id}/${kanban.id}`)

	const cards = page.locator('.kanban .bucket .task')
	const parent = cards.filter({hasText: 'Ship the release'})
	await expect(parent).toBeVisible()
	await expect(cards).toHaveCount(1)
	await expect(parent.locator('.subtask-progress')).toHaveText('1/3')
	await page.screenshot({path: testInfo.outputPath('kanban-hidden-1400.png')})

	await page.getByRole('button', {name: 'Show subtasks'}).click()
	await expect(cards).toHaveCount(3)
	await expect(cards.filter({hasText: 'Tag the build'})).toBeVisible()
	await page.screenshot({path: testInfo.outputPath('kanban-shown-1400.png')})

	// The choice is remembered per view.
	await page.reload()
	await expect(cards).toHaveCount(3)
	await page.getByRole('button', {name: 'Hide subtasks'}).click()
	await expect(cards).toHaveCount(1)

	await page.setViewportSize({width: 400, height: 900})
	await page.reload()
	await expect(parent.locator('.subtask-progress')).toHaveText('1/3')
	await page.screenshot({path: testInfo.outputPath('kanban-hidden-400.png')})

	// Opening the parent lists every child, including the one from another project.
	await page.setViewportSize({width: 1400, height: 900})
	await parent.click()
	const related = page.locator('.task-view .task-relations')
	await expect(related).toContainText('Write the notes')
	await expect(related).toContainText('Update the docs')
	await page.screenshot({path: testInfo.outputPath('parent-detail-1400.png'), fullPage: true})
})
