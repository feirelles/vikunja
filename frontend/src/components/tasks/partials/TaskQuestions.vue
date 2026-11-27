<!-- eslint-disable vue/no-v-html -->
<template>
	<section
		v-if="visibleQuestions.length > 0"
		class="content details task-questions"
		:aria-labelledby="headingId"
	>
		<h2
			:id="headingId"
			class="task-section-title"
		>
			<span class="icon is-grey">
				<Icon :icon="['far', 'circle-question']" />
			</span>
			{{ $t('task.questions.title') }}
		</h2>

		<article
			v-for="q in visibleQuestions"
			:key="q.id"
			class="task-question"
			:class="`is-${q.status}`"
			:data-question-id="q.id"
		>
			<template v-if="q.status === 'answered'">
				<BaseButton
					class="answered-line"
					:aria-expanded="expandedAnswers.has(q.id)"
					@click="toggle(expandedAnswers, q.id)"
				>
					<span class="icon has-text-success">
						<Icon icon="check" />
					</span>
					<span class="answered-title">{{ q.title }}</span>
					<span class="answered-value">{{ answerText(q) }}</span>
					<span class="icon is-grey">
						<Icon :icon="expandedAnswers.has(q.id) ? 'chevron-up' : 'chevron-down'" />
					</span>
				</BaseButton>
				<div
					v-if="expandedAnswers.has(q.id)"
					class="answered-body"
				>
					<p
						v-if="q.summary"
						class="question-summary"
					>
						{{ q.summary }}
					</p>
					<div
						v-if="q.details"
						class="question-details"
						v-html="sanitizeProjectDescription(q.details)"
					/>
					<p
						v-if="q.answer_notes"
						class="answer-notes"
					>
						{{ q.answer_notes }}
					</p>
					<p class="answer-meta">
						{{ answeredLine(q) }}
					</p>
				</div>
			</template>

			<template v-else>
				<header class="question-header">
					<h3 class="question-title">
						{{ q.title }}
					</h3>
					<span
						v-if="q.status === 'deferred'"
						v-tooltip="answeredLine(q)"
						class="tag is-warning"
					>
						{{ $t('task.questions.deferred') }}
					</span>
				</header>
				<p
					v-if="q.summary"
					class="question-summary"
				>
					{{ q.summary }}
				</p>

				<template v-if="q.details">
					<BaseButton
						class="details-toggle has-text-primary"
						:aria-expanded="expandedDetails.has(q.id)"
						:aria-controls="`task-question-${q.id}-details`"
						@click="toggle(expandedDetails, q.id)"
					>
						<Icon :icon="expandedDetails.has(q.id) ? 'chevron-up' : 'chevron-down'" />
						{{ expandedDetails.has(q.id) ? $t('task.questions.hideDetails') : $t('task.questions.showDetails') }}
					</BaseButton>
					<div
						v-if="expandedDetails.has(q.id)"
						:id="`task-question-${q.id}-details`"
						class="question-details"
						v-html="sanitizeProjectDescription(q.details)"
					/>
				</template>

				<div
					v-if="q.options.length > 0"
					class="question-options"
				>
					<component
						:is="canWrite ? BaseButton : 'div'"
						v-for="option in q.options"
						:key="option.key"
						class="question-option"
						:class="{'is-recommended': isRecommended(q, option.key)}"
						:data-option-key="option.key"
						v-bind="canWrite ? {ariaDisabled: isPending} : {}"
						@click="canWrite && answerWith(q, {option: option.key})"
					>
						<span class="option-label">
							{{ option.label }}
							<span
								v-if="isRecommended(q, option.key)"
								class="tag is-primary"
							>
								{{ $t('task.questions.recommended') }}
							</span>
						</span>
						<span
							v-if="option.description"
							class="option-description"
						>
							{{ option.description }}
						</span>
						<span
							v-if="isRecommended(q, option.key) && q.recommendation?.reason"
							class="option-reason"
						>
							{{ q.recommendation.reason }}
						</span>
					</component>
				</div>

				<div
					v-if="canWrite"
					class="question-controls"
				>
					<div class="field">
						<label
							class="label"
							:for="`task-question-${q.id}-other`"
						>
							{{ $t('task.questions.other') }}
						</label>
						<div class="field has-addons">
							<div class="control is-expanded">
								<input
									:id="`task-question-${q.id}-other`"
									v-model="otherDrafts[q.id]"
									class="input"
									type="text"
									:placeholder="$t('task.questions.otherPlaceholder')"
									@keydown.enter.prevent="answerWithOther(q)"
								>
							</div>
							<div class="control">
								<XButton
									variant="secondary"
									class="send-other"
									:aria-disabled="isPending || !otherDrafts[q.id]?.trim() || undefined"
									@click="answerWithOther(q)"
								>
									{{ $t('task.questions.sendOther') }}
								</XButton>
							</div>
						</div>
					</div>
					<div class="field">
						<label
							class="label"
							:for="`task-question-${q.id}-notes`"
						>
							{{ $t('task.questions.notes') }}
						</label>
						<div class="control">
							<textarea
								:id="`task-question-${q.id}-notes`"
								v-model="notesDrafts[q.id]"
								class="textarea"
								rows="2"
								:placeholder="$t('task.questions.notesPlaceholder')"
							/>
						</div>
					</div>
					<XButton
						v-if="q.status === 'open'"
						variant="tertiary"
						class="defer-button"
						icon="history"
						:aria-disabled="isPending || undefined"
						@click="answerWith(q, {defer: true})"
					>
						{{ $t('task.questions.defer') }}
					</XButton>
				</div>
			</template>
		</article>
	</section>
</template>

<script setup lang="ts">
import {computed, reactive, useId} from 'vue'
import {useI18n} from 'vue-i18n'
import {useQuery} from '@tanstack/vue-query'

import BaseButton from '@/components/base/BaseButton.vue'
import XButton from '@/components/input/Button.vue'
import type {TaskQuestionAnswerWritable} from '@/client/generated'
import {
	taskQuestionsQuery,
	useAnswerTaskQuestionMutation,
	type TaskQuestionResponse,
} from '@/client/queries/taskQuestions'
import {sanitizeProjectDescription} from '@/helpers/sanitizeProjectDescription'
import {formatDisplayDate} from '@/helpers/time/formatDate'
import {getDisplayName} from '@/helpers/user'

const props = withDefaults(defineProps<{
	taskId: number,
	canWrite?: boolean,
}>(), {
	canWrite: false,
})

const {t} = useI18n({useScope: 'global'})
const headingId = useId()

const questionsQuery = useQuery(computed(() => taskQuestionsQuery(props.taskId)))
const visibleQuestions = computed(() => (questionsQuery.data.value ?? []).filter(q => q.status !== 'cancelled'))

const answerMutation = useAnswerTaskQuestionMutation()
const isPending = computed(() => answerMutation.isPending.value)

const expandedDetails = reactive(new Set<number>())
const expandedAnswers = reactive(new Set<number>())
const otherDrafts = reactive<Record<number, string>>({})
const notesDrafts = reactive<Record<number, string>>({})

function toggle(set: Set<number>, id: number) {
	if (set.has(id)) {
		set.delete(id)
	} else {
		set.add(id)
	}
}

function isRecommended(q: TaskQuestionResponse, key: string) {
	return q.recommendation?.key === key
}

function answerText(q: TaskQuestionResponse) {
	if (q.answer_option) {
		return q.options.find(option => option.key === q.answer_option)?.label ?? q.answer_option
	}
	return q.answer_other
}

function answeredLine(q: TaskQuestionResponse) {
	const params = {
		user: getDisplayName(q.answered_by),
		date: formatDisplayDate(q.answered_at),
	}
	return q.status === 'deferred'
		? t('task.questions.deferredBy', params)
		: t('task.questions.answeredBy', params)
}

async function answerWith(q: TaskQuestionResponse, answer: TaskQuestionAnswerWritable) {
	if (isPending.value) return
	const notes = notesDrafts[q.id] ?? ''
	const other = otherDrafts[q.id] ?? ''
	try {
		await answerMutation.mutateAsync({
			taskId: props.taskId,
			questionId: q.id,
			answer: {
				...answer,
				...(notes.trim() === '' ? {} : {notes: notes.trim()}),
			},
		})
	} catch {
		return
	}
	// The user may have kept typing while the request was pending.
	if (notesDrafts[q.id] === notes) delete notesDrafts[q.id]
	if (otherDrafts[q.id] === other) delete otherDrafts[q.id]
}

function answerWithOther(q: TaskQuestionResponse) {
	const other = otherDrafts[q.id]?.trim() ?? ''
	if (other === '') return
	return answerWith(q, {other})
}
</script>

<style scoped lang="scss">
.task-questions {
	margin-block-end: 1.5rem;
}

.task-question {
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	padding: .75rem 1rem;
	margin-block-end: .75rem;

	&.is-deferred {
		border-color: hsla(var(--warning-h), var(--warning-s), var(--warning-l), .6);
	}

	&.is-answered {
		padding: .5rem 1rem;
		background: var(--grey-50);
	}
}

.question-header {
	display: flex;
	align-items: center;
	gap: .5rem;
	flex-wrap: wrap;
}

.content .question-title {
	font-size: 1.1rem;
	margin: 0;
}

.question-summary {
	margin-block: .25rem .5rem;
	color: var(--text-muted);
}

.details-toggle {
	display: inline-flex;
	align-items: center;
	gap: .25rem;
	font-size: .875rem;
	margin-block-end: .5rem;
}

.question-details {
	padding: .5rem .75rem;
	margin-block-end: .75rem;
	border-inline-start: 3px solid var(--grey-200);
}

.question-options {
	display: grid;
	gap: .5rem;
	margin-block: .5rem 1rem;
}

.question-option {
	display: flex;
	flex-direction: column;
	align-items: flex-start;
	gap: .125rem;
	inline-size: 100%;
	padding: .5rem .75rem;
	border: 1px solid var(--grey-200);
	border-radius: $radius;
	background: transparent;
	color: inherit;
	text-align: start;

	&.base-button {
		cursor: pointer;
		transition: border-color $transition, background-color $transition;

		&:hover:not([aria-disabled="true"]),
		&:focus-visible {
			border-color: var(--primary);
		}

		&[aria-disabled="true"] {
			cursor: wait;
			opacity: .6;
		}
	}

	&.is-recommended {
		border: 2px solid var(--primary);
		background: hsla(var(--primary-hsl), .08);
	}
}

.option-label {
	display: inline-flex;
	align-items: center;
	gap: .5rem;
	font-weight: 600;
}

.option-description {
	font-size: .875rem;
	color: var(--text-muted);
}

.option-reason {
	font-size: .875rem;
	font-style: italic;
}

.question-controls {
	.label {
		font-size: .875rem;
	}

	.field.has-addons {
		margin-block-end: 0;
	}
}

.answered-line {
	display: flex;
	align-items: center;
	gap: .5rem;
	inline-size: 100%;
	text-align: start;
}

.answered-title {
	font-weight: 600;
}

.answered-value {
	flex: 1;
	min-inline-size: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	color: var(--text-muted);
}

.answered-body {
	padding-block: .5rem 0;
	padding-inline-start: 2rem;
}

.answer-notes {
	white-space: pre-wrap;
	margin-block-end: .25rem;
}

.answer-meta {
	font-size: .875rem;
	color: var(--text-muted);
}
</style>
