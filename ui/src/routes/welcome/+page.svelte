<script lang="ts">
	import logoSvg from '$lib/assets/logo.svg?raw';
</script>

<svelte:head>
	<title>Pebble</title>
</svelte:head>

<div class="flex min-h-screen flex-col items-center justify-center bg-background px-6 py-12">
	<div class="w-full max-w-sm rounded-xl border border-border bg-card p-8 shadow-sm">
		<div class="flex flex-col items-center gap-5 text-center">
			<div class="flex h-14 w-14 items-center justify-center rounded-xl bg-primary/5 border border-border p-3 text-foreground">
				{@html logoSvg}
			</div>

			<div class="flex flex-col items-center gap-1">
				<h1 class="text-base font-semibold text-foreground tracking-tight">Pebble</h1>
				<p class="text-[13px] text-muted-foreground leading-relaxed">
					Your reverse proxy is running. This domain isn't configured yet.
				</p>
			</div>

			<div class="w-full rounded-lg border border-border bg-muted/30 px-4 py-3">
				<div class="flex items-center justify-between">
					<span class="text-xs text-muted-foreground">Proxy status</span>
					<div class="inline-flex items-center gap-1.5">
						<span class="h-1.5 w-1.5 rounded-full bg-green-500 animate-pulse"></span>
						<span class="text-xs font-medium text-foreground">Running</span>
					</div>
				</div>
			</div>

			<div class="w-full space-y-2 text-left">
				<div class="flex items-center justify-between rounded-lg border border-border/50 px-3 py-2">
					<span class="text-[11px] text-muted-foreground">Server</span>
					<span class="text-[11px] font-mono text-foreground">Caddy</span>
				</div>
				<div class="flex items-center justify-between rounded-lg border border-border/50 px-3 py-2">
					<span class="text-[11px] text-muted-foreground">Host</span>
					<span class="text-[11px] font-mono text-foreground" id="pebble-host">—</span>
				</div>
			</div>
		</div>
	</div>

	<div class="mt-6 flex items-center gap-1.5 text-muted-foreground/40">
		<div class="h-3 w-3">
			{@html logoSvg}
		</div>
		<span class="text-[10px] tracking-wide uppercase font-medium">Managed by Pebble</span>
	</div>
</div>

{@html '<script>document.getElementById("pebble-host").textContent=location.hostname</script>'}
