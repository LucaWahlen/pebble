<script lang="ts">
	import { cn } from '$lib/utils';
	import type { Snippet } from 'svelte';

	interface Props {
		content: string;
		class?: string;
		children: Snippet;
	}

	let { content, class: className = '', children }: Props = $props();
	let visible = $state(false);
</script>

<div
	class={cn('relative inline-flex', className)}
	onmouseenter={() => (visible = true)}
	onmouseleave={() => (visible = false)}
	role="tooltip"
>
	{@render children()}
	{#if visible}
		<div
			class="absolute bottom-full left-1/2 z-50 mb-2 -translate-x-1/2 rounded-md border bg-popover px-3 py-1.5 text-xs text-popover-foreground shadow-md whitespace-nowrap"
		>
			{content}
		</div>
	{/if}
</div>
