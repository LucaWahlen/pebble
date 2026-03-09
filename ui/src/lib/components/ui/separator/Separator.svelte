<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';
	import { cn } from '$lib/utils';

	type Orientation = 'horizontal' | 'vertical';

	interface Props extends HTMLAttributes<HTMLDivElement> {
		orientation?: Orientation;
	}

	let { orientation = 'horizontal', class: className = '', ...restProps }: Props = $props();
</script>

<div
	role="separator"
	aria-orientation={orientation}
	class={cn(
		'bg-border shrink-0',
		orientation === 'vertical' ? 'w-px' : 'h-px w-full',
		className
	)}
	{...restProps}
></div>
