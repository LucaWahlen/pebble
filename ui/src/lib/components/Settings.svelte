<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Separator } from '$lib/components/ui/separator';
	import {
		X,
		Github,
		Loader2,
		Check,
		AlertCircle,
		ArrowDownToLine,
		ArrowUpFromLine,
		TestTube,
		ChevronRight
	} from 'lucide-svelte';

	interface Props {
		open: boolean;
		onclose: () => void;
		onSyncChange?: (syncing: boolean) => void;
	}

	let { open = $bindable(false), onclose, onSyncChange }: Props = $props();

	let githubToken = $state('');
	let githubRepo = $state('');
	let githubBranch = $state('main');
	let syncEnabled = $state(true);
	let hasToken = $state(false);

	let testStatus = $state<'idle' | 'testing' | 'success' | 'error'>('idle');
	let testError = $state('');
	let saveStatus = $state<'idle' | 'saving' | 'saved' | 'error'>('idle');
	let pullStatus = $state<'idle' | 'pulling' | 'success' | 'error'>('idle');
	let pullMessage = $state('');
	let pushStatus = $state<'idle' | 'pushing' | 'success' | 'error'>('idle');
	let pushMessage = $state('');

	let advancedOpen = $state(false);
	let configLoaded = $state(false);

	async function loadConfig() {
		try {
			const res = await fetch('/api/config');
			if (!res.ok) return;
			const data = await res.json();
			githubRepo = data.githubRepo || '';
			githubBranch = data.githubBranch || 'main';
			syncEnabled = data.syncEnabled ?? true;
			hasToken = data.hasToken || false;
			configLoaded = true;
		} catch (e) {
			console.error('Failed to load config:', e);
		}
	}

	$effect(() => {
		if (open && !configLoaded) {
			loadConfig();
		}
	});

	async function testConnection() {
		testStatus = 'testing';
		testError = '';
		try {
			const res = await fetch('/api/github/test', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					githubToken: githubToken || undefined,
					githubRepo,
					githubBranch
				})
			});
			const data = await res.json();
			if (data.success) {
				testStatus = 'success';
				setTimeout(() => (testStatus = 'idle'), 3000);
			} else {
				testStatus = 'error';
				testError = data.error || 'Connection test failed';
				setTimeout(() => (testStatus = 'idle'), 5000);
			}
		} catch (e) {
			testStatus = 'error';
			testError = 'Request failed';
			setTimeout(() => (testStatus = 'idle'), 5000);
		}
	}

	async function saveConfig() {
		saveStatus = 'saving';
		try {
			const body: Record<string, unknown> = {
				githubRepo,
				githubBranch,
				syncEnabled
			};
			if (githubToken) {
				body.githubToken = githubToken;
			}
			const res = await fetch('/api/config', {
				method: 'PUT',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(body)
			});
			if (!res.ok) throw new Error('Save failed');
			saveStatus = 'saved';
			if (githubToken) {
				hasToken = true;
				githubToken = '';
			}
			onSyncChange?.(syncEnabled);
			setTimeout(() => (saveStatus = 'idle'), 2000);
		} catch (e) {
			saveStatus = 'error';
			setTimeout(() => (saveStatus = 'idle'), 3000);
		}
	}

	async function pullNow() {
		pullStatus = 'pulling';
		pullMessage = '';
		try {
			const res = await fetch('/api/github/pull', { method: 'POST' });
			const data = await res.json();
			if (data.success) {
				pullStatus = 'success';
				pullMessage = `Pulled ${data.count} file(s)`;
				setTimeout(() => (pullStatus = 'idle'), 3000);
			} else {
				pullStatus = 'error';
				pullMessage = data.error || 'Pull failed';
				setTimeout(() => (pullStatus = 'idle'), 5000);
			}
		} catch (e) {
			pullStatus = 'error';
			pullMessage = 'Request failed';
			setTimeout(() => (pullStatus = 'idle'), 5000);
		}
	}

	async function pushNow() {
		pushStatus = 'pushing';
		pushMessage = '';
		try {
			const res = await fetch('/api/github/push', { method: 'POST' });
			const data = await res.json();
			if (data.success) {
				pushStatus = 'success';
				pushMessage = 'Pushed successfully';
				setTimeout(() => (pushStatus = 'idle'), 3000);
			} else {
				pushStatus = 'error';
				pushMessage = data.error || 'Push failed';
				setTimeout(() => (pushStatus = 'idle'), 8000);
			}
		} catch (e) {
			pushStatus = 'error';
			pushMessage = 'Request failed';
			setTimeout(() => (pushStatus = 'idle'), 5000);
		}
	}
</script>

{#if open}
	<!-- Backdrop -->
	<button
		class="fixed inset-0 z-40 bg-black/50 backdrop-blur-sm"
		onclick={onclose}
		aria-label="Close settings"
	></button>

	<!-- Modal -->
	<div class="fixed inset-y-0 right-0 z-50 flex w-full max-w-md flex-col bg-background border-l border-border shadow-xl">
		<!-- Header -->
		<div class="flex h-12 items-center gap-3 border-b border-border px-4 shrink-0">
			<Github class="h-4 w-4" />
			<span class="text-sm font-semibold">Settings</span>
			<div class="flex-1"></div>
			<button
				onclick={onclose}
				class="flex h-7 w-7 items-center justify-center rounded hover:bg-accent transition-colors"
				aria-label="Close"
			>
				<X class="h-4 w-4" />
			</button>
		</div>

		<!-- Content -->
		<div class="flex-1 overflow-y-auto p-4 space-y-6">
			<!-- GitHub Sync -->
			<div class="space-y-4">
				<div>
					<h3 class="text-sm font-semibold">GitHub Sync</h3>
					<p class="text-xs text-muted-foreground mt-1">
						Connect a GitHub repository to sync your Caddy configuration files.
						When enabled, Pebble automatically pulls remote changes and pushes local changes every 60 seconds. Caddy is reloaded on each pull.
					</p>
				</div>

				<!-- Token -->
				<div class="space-y-1.5">
					<label for="gh-token" class="text-xs font-medium text-muted-foreground">
						Personal Access Token
						{#if hasToken}
							<span class="text-green-500 ml-1">● set</span>
						{/if}
					</label>
					<input
						id="gh-token"
						type="password"
						bind:value={githubToken}
						placeholder={hasToken ? '••••••••••••' : 'ghp_...'}
						class="w-full rounded-md border border-border bg-background px-3 py-1.5 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-ring"
					/>
					<p class="text-[10px] text-muted-foreground">
						Classic token: <code class="bg-muted px-1 py-0.5 rounded">repo</code> scope. Fine-grained: <code class="bg-muted px-1 py-0.5 rounded">Contents: Read and Write</code>. Leave empty to keep existing.
					</p>
				</div>

				<!-- Repo -->
				<div class="space-y-1.5">
					<label for="gh-repo" class="text-xs font-medium text-muted-foreground">Repository</label>
					<input
						id="gh-repo"
						type="text"
						bind:value={githubRepo}
						placeholder="owner/repo"
						class="w-full rounded-md border border-border bg-background px-3 py-1.5 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-ring"
					/>
				</div>

				<!-- Branch -->
				<div class="space-y-1.5">
					<label for="gh-branch" class="text-xs font-medium text-muted-foreground">Branch</label>
					<input
						id="gh-branch"
						type="text"
						bind:value={githubBranch}
						placeholder="main"
						class="w-full rounded-md border border-border bg-background px-3 py-1.5 text-xs font-mono focus:outline-none focus:ring-1 focus:ring-ring"
					/>
				</div>

				<!-- Enable sync toggle -->
				<div class="flex items-center justify-between">
					<div>
						<span class="text-xs font-medium">Auto-sync</span>
						<p class="text-[10px] text-muted-foreground">Pull & push every 60 seconds, auto-reload Caddy</p>
					</div>
					<button
						role="switch"
						aria-checked={syncEnabled}
						onclick={() => (syncEnabled = !syncEnabled)}
						class="relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border border-border transition-colors {syncEnabled ? 'bg-primary' : 'bg-muted'}"
					>
						<span
							class="pointer-events-none block h-3.5 w-3.5 rounded-full bg-background shadow transition-transform {syncEnabled ? 'translate-x-4' : 'translate-x-0.5'}"
						></span>
					</button>
				</div>

				<!-- Actions -->
				<div class="flex flex-wrap gap-2">
					<Button
						variant="outline"
						size="sm"
						onclick={testConnection}
						disabled={testStatus === 'testing' || (!githubRepo)}
						class="h-7 gap-1.5 text-xs"
					>
						{#if testStatus === 'testing'}
							<Loader2 class="h-3 w-3 animate-spin" />
						{:else if testStatus === 'success'}
							<Check class="h-3 w-3 text-green-500" />
						{:else if testStatus === 'error'}
							<AlertCircle class="h-3 w-3 text-destructive" />
						{:else}
							<TestTube class="h-3 w-3" />
						{/if}
						Test
					</Button>

					<Button
						variant="outline"
						size="sm"
						onclick={saveConfig}
						disabled={saveStatus === 'saving'}
						class="h-7 gap-1.5 text-xs"
					>
						{#if saveStatus === 'saving'}
							<Loader2 class="h-3 w-3 animate-spin" />
						{:else if saveStatus === 'saved'}
							<Check class="h-3 w-3 text-green-500" />
						{:else if saveStatus === 'error'}
							<AlertCircle class="h-3 w-3 text-destructive" />
						{:else}
							<Check class="h-3 w-3" />
						{/if}
						Save
					</Button>
				</div>

				{#if testStatus === 'error' && testError}
					<p class="text-xs text-destructive">{testError}</p>
				{/if}

				<Separator />

				<!-- Collapsible Manual Sync -->
				<div>
					<button
						onclick={() => (advancedOpen = !advancedOpen)}
						class="flex w-full items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors py-1"
					>
						<ChevronRight class="h-3 w-3 transition-transform {advancedOpen ? 'rotate-90' : ''}" />
						Manual Sync
					</button>
					{#if advancedOpen}
						<div class="mt-2 space-y-3 pl-4.5">
							<p class="text-[10px] text-muted-foreground">
								Manually pull or push files. Use this if auto-sync is off or you need an immediate sync.
							</p>
							<div class="flex flex-wrap gap-2">
								<Button
									variant="outline"
									size="sm"
									onclick={pullNow}
									disabled={pullStatus === 'pulling' || !hasToken || !githubRepo}
									class="h-7 gap-1.5 text-xs"
								>
									{#if pullStatus === 'pulling'}
										<Loader2 class="h-3 w-3 animate-spin" />
									{:else if pullStatus === 'success'}
										<Check class="h-3 w-3 text-green-500" />
									{:else if pullStatus === 'error'}
										<AlertCircle class="h-3 w-3 text-destructive" />
									{:else}
										<ArrowDownToLine class="h-3 w-3" />
									{/if}
									Pull from GitHub
								</Button>

								<Button
									variant="outline"
									size="sm"
									onclick={pushNow}
									disabled={pushStatus === 'pushing' || !hasToken || !githubRepo}
									class="h-7 gap-1.5 text-xs"
								>
									{#if pushStatus === 'pushing'}
										<Loader2 class="h-3 w-3 animate-spin" />
									{:else if pushStatus === 'success'}
										<Check class="h-3 w-3 text-green-500" />
									{:else if pushStatus === 'error'}
										<AlertCircle class="h-3 w-3 text-destructive" />
									{:else}
										<ArrowUpFromLine class="h-3 w-3" />
									{/if}
									Push to GitHub
								</Button>
							</div>
							{#if pullMessage}
								<p class="text-xs {pullStatus === 'error' ? 'text-destructive' : 'text-muted-foreground'}">
									{pullMessage}
								</p>
							{/if}
							{#if pushMessage}
								<p class="text-xs {pushStatus === 'error' ? 'text-destructive' : 'text-muted-foreground'}">
									{pushMessage}
								</p>
							{/if}
						</div>
					{/if}
				</div>
			</div>
		</div>
	</div>
{/if}

