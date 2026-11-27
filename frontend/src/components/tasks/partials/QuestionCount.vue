<template>
	<span
		v-tooltip="label"
		class="question-count"
		role="img"
		:aria-label="label"
	>
		<Icon :icon="['far', 'circle-question']" />
		<span aria-hidden="true">{{ $t('task.questions.count', count) }}</span>
	</span>
</template>

<script setup lang="ts">
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'

const props = defineProps<{
	count: number
}>()

const {t} = useI18n({useScope: 'global'})

const label = computed(() => t('task.questions.openCount', props.count))
</script>

<style scoped lang="scss">
.question-count {
	display: inline-flex;
	align-items: center;
	gap: .25rem;
	padding: 0 .5rem;
	border-radius: $radius;
	background: hsla(var(--warning-h), var(--warning-s), var(--warning-l), .2);
	color: inherit;
	font-size: .75rem;
	font-weight: 600;
	line-height: 1.5rem;
}
</style>
