<script lang="ts">
import { onMount } from 'svelte';
import { toggleMode, mode } from 'mode-watcher';
import Editor from '$lib/components/Editor.svelte';
import FileTree from '$lib/components/FileTree.svelte';
import { Button } from '$lib/components/ui/button';
import { Tooltip } from '$lib/components/ui/tooltip';
import { Separator } from '$lib/components/ui/separator';
import {
	Sun,
	Moon,
	File,
	Loader2,
	Check,
	AlertCircle,
	X,
	PanelLeft,
	Save,
	Github,
	RefreshCw,
	FilePlus,
	FolderPlus,
	LogOut
} from 'lucide-svelte';
import logoSvg from '$lib/assets/logo.svg?raw';
import SettingsPanel from '$lib/components/Settings.svelte';
import { caddyfile } from '$lib/lang-caddyfile';
import type { FileEntry, FileNode } from '$lib/types';
import type FileTreeComponent from '$lib/components/FileTree.svelte';

// ── Auth state ──
let authRequired = $state(false);
let authenticated = $state(false);
let authChecked = $state(false);
let needsSetup = $state(false);
let loginPassword = $state('');
let loginError = $state('');
let loginLoading = $state(false);
let setupPassword = $state('');
let setupConfirm = $state('');
let setupError = $state('');
let setupLoading = $state(false);

// Types for staged operations
interface StagedOp {
	type: 'create' | 'delete' | 'rename' | 'move';
	path: string;
	newPath?: string;
	isDir?: boolean;
}

// State — real server files
let serverFiles = $state<FileEntry[]>([]);
let fileTreeRef = $state<FileTreeComponent>();
let selectedFile = $state<string | null>(null);
let editorContent = $state('');
let originalContent = $state('');
let loading = $state(false);
let loadingFiles = $state(true);
let expandedDirs = $state<Set<string>>(new Set());
let caddyError = $state<string | null>(null);
let sidebarOpen = $state(true);
let settingsOpen = $state(false);
let syncActive = $state(false);
let applyStatus = $state<'idle' | 'applying' | 'success' | 'error'>('idle');
let applyError = $state<string | null>(null);

// Staged changes
let stagedOps = $state<StagedOp[]>([]);
let stagedContent = $state<Map<string, string>>(new Map());

// Map from current virtual path -> original server path (for files that were renamed/moved)
let pathMapping = $state<Map<string, string>>(new Map());

const hasPending = $derived(stagedOps.length > 0 || stagedContent.size > 0);
const isDirty = $derived(selectedFile !== null && editorContent !== originalContent);
const currentTheme = $derived(mode.current === 'dark' ? 'dark' : 'light');
const editorLang = $derived(selectedFile ? caddyfile() : null);

// Build virtual file list by replaying staged ops on top of server files
const virtualFiles = $derived.by(() => {
	let entries = serverFiles.map(f => ({ ...f }));

	for (const op of stagedOps) {
		switch (op.type) {
			case 'create':
				if (op.isDir) {
					const dirName = op.path.split('/').pop() || op.path;
					entries.push({ name: dirName, path: op.path, isDirectory: true });
				} else {
					const fileName = op.path.split('/').pop() || op.path;
					entries.push({ name: fileName, path: op.path, isDirectory: false });
				}
				break;

			case 'delete':
				entries = entries.filter(f => f.path !== op.path && !f.path.startsWith(op.path + '/'));
				break;

			case 'rename':
			case 'move':
				if (!op.newPath) break;
				entries = entries.map(f => {
					if (f.path === op.path) {
						const newName = op.newPath!.split('/').pop() || f.name;
						return { ...f, path: op.newPath!, name: newName };
					}
					if (f.path.startsWith(op.path + '/')) {
						const suffix = f.path.slice(op.path.length);
						const newChildPath = op.newPath! + suffix;
						const newName = newChildPath.split('/').pop() || f.name;
						return { ...f, path: newChildPath, name: newName };
					}
					return f;
				});
				break;
		}
	}

	return entries;
});

// All known virtual paths for conflict detection
const allPaths = $derived(new Set(virtualFiles.map(f => f.path)));

const fileTree = $derived(buildTree(virtualFiles));

// Count pending changes for display
const pendingCount = $derived(stagedOps.length + stagedContent.size);

function stageCurrentFile() {
	if (!selectedFile) return;
	if (editorContent !== originalContent) {
		stagedContent.set(selectedFile, editorContent);
	} else {
		stagedContent.delete(selectedFile);
	}
	stagedContent = new Map(stagedContent);
}

function buildTree(entries: FileEntry[]): FileNode[] {
	const root: FileNode[] = [];
	const nodeMap = new Map<string, FileNode>();
	const sorted = [...entries].sort((a, b) => {
		const depthA = a.path.split('/').length;
		const depthB = b.path.split('/').length;
		if (depthA !== depthB) return depthA - depthB;
		if (a.isDirectory !== b.isDirectory) return a.isDirectory ? -1 : 1;
		return a.name.localeCompare(b.name);
	});
	for (const entry of sorted) {
		const node: FileNode = {
			name: entry.name,
			path: entry.path,
			isDirectory: entry.isDirectory,
			size: entry.size,
			modifiedAt: entry.modifiedAt,
			children: entry.isDirectory ? [] : undefined
		};
		nodeMap.set(entry.path, node);
		const parts = entry.path.split('/').filter(Boolean);
		if (parts.length === 1) {
			root.push(node);
		} else {
			const parentPath = parts.slice(0, -1).join('/');
			const parent = nodeMap.get(parentPath);
			if (parent?.children) {
				parent.children.push(node);
			} else {
				root.push(node);
			}
		}
	}

	function sortChildren(nodes: FileNode[]) {
		nodes.sort((a, b) => {
			if (a.isDirectory !== b.isDirectory) return a.isDirectory ? -1 : 1;
			return a.name.localeCompare(b.name);
		});
		for (const n of nodes) {
			if (n.children) sortChildren(n.children);
		}
	}
	sortChildren(root);
	return root;
}

async function loadFiles() {
	loadingFiles = true;
	try {
		const res = await fetch('/api/files');
		const data = await res.json();
		serverFiles = data.files;
		const dirs = new Set<string>();
		for (const f of data.files) {
			if (f.isDirectory) dirs.add(f.path);
		}
		for (const d of dirs) expandedDirs.add(d);
		expandedDirs = new Set(expandedDirs);
	} catch (e) {
		console.error('Failed to load files:', e);
	} finally {
		loadingFiles = false;
	}
}

// Resolve a virtual path to its original server path
function resolveOriginalPath(virtualPath: string): string {
	return pathMapping.get(virtualPath) ?? virtualPath;
}

async function openFile(path: string) {
	if (selectedFile && editorContent !== originalContent) {
		stagedContent.set(selectedFile, editorContent);
		stagedContent = new Map(stagedContent);
	}
	loading = true;
	try {
		const isNew = stagedOps.some(op => op.type === 'create' && !op.isDir && op.path === path);

		if (isNew) {
			originalContent = '';
			editorContent = stagedContent.get(path) ?? '';
		} else if (stagedContent.has(path)) {
			editorContent = stagedContent.get(path)!;
			const serverPath = resolveOriginalPath(path);
			const res = await fetch(`/api/files/${serverPath}`);
			if (!res.ok) throw new Error('Failed to load file');
			const data = await res.json();
			originalContent = data.content;
		} else {
			const serverPath = resolveOriginalPath(path);
			const res = await fetch(`/api/files/${serverPath}`);
			if (!res.ok) throw new Error('Failed to load file');
			const data = await res.json();
			editorContent = data.content;
			originalContent = data.content;
		}
		selectedFile = path;
	} catch (e) {
		console.error('Failed to open file:', e);
	} finally {
		loading = false;
	}
}

async function applyChanges() {
	if (!hasPending) return;
	applyStatus = 'applying';
	applyError = null;
	caddyError = null;
	try {
		const operations = stagedOps.map(op => {
			if (op.type === 'rename' || op.type === 'move') {
				return { type: op.type, path: op.path, newPath: op.newPath, isDir: op.isDir };
			}
			return { type: op.type, path: op.path, isDir: op.isDir };
		});

		const files = Array.from(stagedContent.entries()).map(([path, content]) => ({ path, content }));

		const res = await fetch('/api/apply', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ operations, files })
		});
		const data = await res.json();
		if (!res.ok || !data.success) {
			applyStatus = 'error';
			applyError = data.error ?? 'Apply failed';
			setTimeout(() => (applyStatus = 'idle'), 3000);
			return;
		}
		if (data.caddyError) caddyError = data.caddyError;

		if (selectedFile && stagedContent.has(selectedFile)) originalContent = editorContent;
		stagedOps = [];
		stagedContent = new Map();
		pathMapping = new Map();
		applyStatus = 'success';
		await loadFiles();
		setTimeout(() => (applyStatus = 'idle'), 2000);
	} catch (e) {
		applyStatus = 'error';
		applyError = e instanceof Error ? e.message : 'Apply failed';
		setTimeout(() => (applyStatus = 'idle'), 3000);
	}
}

function handleCreate(fullPath: string, type: 'file' | 'folder', _parentDir: string) {
	if (allPaths.has(fullPath)) return;

	stagedOps = [...stagedOps, { type: 'create', path: fullPath, isDir: type === 'folder' }];

	if (type === 'folder') {
		expandedDirs.add(fullPath);
		expandedDirs = new Set(expandedDirs);
	}

	if (type === 'file') {
		stagedContent.set(fullPath, '');
		stagedContent = new Map(stagedContent);
		openFile(fullPath);
	}
}

function handleRename(oldPath: string, newName: string) {
	const parts = oldPath.split('/');
	parts[parts.length - 1] = newName;
	const newPath = parts.join('/');
	if (newPath === oldPath) return;
	if (allPaths.has(newPath)) return;

	stagedOps = [...stagedOps, { type: 'rename', path: oldPath, newPath }];

	const newContent = new Map<string, string>();
	for (const [p, c] of stagedContent) {
		if (p === oldPath) {
			newContent.set(newPath, c);
		} else if (p.startsWith(oldPath + '/')) {
			newContent.set(newPath + p.slice(oldPath.length), c);
		} else {
			newContent.set(p, c);
		}
	}
	stagedContent = newContent;

	const newMapping = new Map<string, string>();
	for (const [virt, orig] of pathMapping) {
		if (virt === oldPath) {
			newMapping.set(newPath, orig);
		} else if (virt.startsWith(oldPath + '/')) {
			newMapping.set(newPath + virt.slice(oldPath.length), orig);
		} else {
			newMapping.set(virt, orig);
		}
	}
	if (!pathMapping.has(oldPath)) {
		newMapping.set(newPath, oldPath);
	}
	for (const f of virtualFiles) {
		if (f.path.startsWith(oldPath + '/') && !pathMapping.has(f.path)) {
			const newChildPath = newPath + f.path.slice(oldPath.length);
			newMapping.set(newChildPath, f.path);
		}
	}
	pathMapping = newMapping;

	if (selectedFile === oldPath) selectedFile = newPath;
	else if (selectedFile?.startsWith(oldPath + '/')) selectedFile = newPath + selectedFile.slice(oldPath.length);

	if (expandedDirs.has(oldPath)) {
		expandedDirs.delete(oldPath);
		expandedDirs.add(newPath);
	}
	const newExpanded = new Set<string>();
	for (const d of expandedDirs) {
		if (d.startsWith(oldPath + '/')) {
			newExpanded.add(newPath + d.slice(oldPath.length));
		} else {
			newExpanded.add(d);
		}
	}
	expandedDirs = newExpanded;
}

function handleDelete(path: string) {
	stagedOps = [...stagedOps, { type: 'delete', path }];

	const newContent = new Map<string, string>();
	for (const [p, c] of stagedContent) {
		if (p !== path && !p.startsWith(path + '/')) {
			newContent.set(p, c);
		}
	}
	stagedContent = newContent;

	const newMapping = new Map<string, string>();
	for (const [virt, orig] of pathMapping) {
		if (virt !== path && !virt.startsWith(path + '/')) {
			newMapping.set(virt, orig);
		}
	}
	pathMapping = newMapping;

	if (selectedFile === path || selectedFile?.startsWith(path + '/')) {
		selectedFile = null;
		editorContent = '';
		originalContent = '';
	}
}

function handleMove(sourcePath: string, targetDir: string) {
	const fileName = sourcePath.split('/').pop();
	if (!fileName) return;
	const newPath = targetDir ? `${targetDir}/${fileName}` : fileName;
	if (newPath === sourcePath) return;
	if (allPaths.has(newPath)) return;

	stagedOps = [...stagedOps, { type: 'move', path: sourcePath, newPath }];

	const newContent = new Map<string, string>();
	for (const [p, c] of stagedContent) {
		if (p === sourcePath) {
			newContent.set(newPath, c);
		} else if (p.startsWith(sourcePath + '/')) {
			newContent.set(newPath + p.slice(sourcePath.length), c);
		} else {
			newContent.set(p, c);
		}
	}
	stagedContent = newContent;

	const newMapping = new Map<string, string>();
	for (const [virt, orig] of pathMapping) {
		if (virt === sourcePath) {
			newMapping.set(newPath, orig);
		} else if (virt.startsWith(sourcePath + '/')) {
			newMapping.set(newPath + virt.slice(sourcePath.length), orig);
		} else {
			newMapping.set(virt, orig);
		}
	}
	if (!pathMapping.has(sourcePath)) {
		newMapping.set(newPath, sourcePath);
	}
	for (const f of virtualFiles) {
		if (f.path.startsWith(sourcePath + '/') && !pathMapping.has(f.path)) {
			const newChildPath = newPath + f.path.slice(sourcePath.length);
			newMapping.set(newChildPath, f.path);
		}
	}
	pathMapping = newMapping;

	if (selectedFile === sourcePath) selectedFile = newPath;
	else if (selectedFile?.startsWith(sourcePath + '/')) selectedFile = newPath + selectedFile.slice(sourcePath.length);

	const newExpanded = new Set<string>();
	for (const d of expandedDirs) {
		if (d === sourcePath) {
			newExpanded.add(newPath);
		} else if (d.startsWith(sourcePath + '/')) {
			newExpanded.add(newPath + d.slice(sourcePath.length));
		} else {
			newExpanded.add(d);
		}
	}
	expandedDirs = newExpanded;
}

function toggleDir(path: string) {
	if (expandedDirs.has(path)) expandedDirs.delete(path);
	else expandedDirs.add(path);
	expandedDirs = new Set(expandedDirs);
}

async function checkSyncStatus() {
	try {
		const res = await fetch('/api/config');
		if (!res.ok) return;
		const data = await res.json();
		syncActive = data.syncEnabled && data.hasToken && !!data.githubRepo;
	} catch { /* ignore */ }
}

function handleKeydown(e: KeyboardEvent) {
	if ((e.ctrlKey || e.metaKey) && e.key === 's') {
		e.preventDefault();
		if (hasPending) applyChanges();
	}
}

// ── Auth functions ──

async function checkAuth() {
	try {
		const res = await fetch('/api/auth/check');
		const data = await res.json();
		authRequired = data.authRequired;
		authenticated = data.authenticated;
		needsSetup = data.needsSetup ?? false;
	} catch {
		authRequired = false;
		authenticated = true;
		needsSetup = false;
	}
	authChecked = true;
}

async function login() {
	loginLoading = true;
	loginError = '';
	try {
		const res = await fetch('/api/auth/login', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ password: loginPassword })
		});
		const data = await res.json();
		if (data.success) {
			authenticated = true;
			loginPassword = '';
			loadFiles();
			checkSyncStatus();
		} else {
			loginError = data.error || 'Login failed';
		}
	} catch {
		loginError = 'Connection failed';
	} finally {
		loginLoading = false;
	}
}

async function logout() {
	await fetch('/api/auth/logout', { method: 'POST' });
	authenticated = false;
	serverFiles = [];
	selectedFile = null;
	editorContent = '';
	originalContent = '';
	stagedOps = [];
	stagedContent = new Map();
	pathMapping = new Map();
}

async function setup() {
	setupError = '';
	if (setupPassword.length < 8) {
		setupError = 'Password must be at least 8 characters';
		return;
	}
	if (setupPassword !== setupConfirm) {
		setupError = 'Passwords do not match';
		return;
	}
	setupLoading = true;
	try {
		const res = await fetch('/api/auth/setup', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ password: setupPassword })
		});
		const data = await res.json();
		if (data.success) {
			needsSetup = false;
			authenticated = true;
			setupPassword = '';
			setupConfirm = '';
			loadFiles();
			checkSyncStatus();
		} else {
			setupError = data.error || 'Setup failed';
		}
	} catch {
		setupError = 'Connection failed';
	} finally {
		setupLoading = false;
	}
}

onMount(() => {
	// Intercept 401 responses globally to force re-login
	const _origFetch = window.fetch;
	window.fetch = async (...args) => {
		const res = await _origFetch(...args);
		if (res.status === 401 && authRequired) {
			authenticated = false;
		}
		return res;
	};

	checkAuth().then(() => {
		if (!authRequired || authenticated) {
			loadFiles();
			checkSyncStatus();
		}
	});

	window.addEventListener('keydown', handleKeydown);
	const handleBeforeUnload = (e: BeforeUnloadEvent) => {
		if (hasPending) {
			e.preventDefault();
		}
	};
	window.addEventListener('beforeunload', handleBeforeUnload);
	if (window.innerWidth < 768) sidebarOpen = false;
	return () => {
		window.removeEventListener('keydown', handleKeydown);
		window.removeEventListener('beforeunload', handleBeforeUnload);
	};
});
</script>

{#if !authChecked}
<!-- Auth check in progress -->
<div class="flex h-screen w-screen items-center justify-center bg-background">
	<Loader2 class="h-6 w-6 animate-spin text-muted-foreground" />
</div>

{:else if needsSetup}
<!-- Initial setup screen -->
<div class="flex min-h-screen flex-col items-center justify-center bg-background px-6 py-12">
	<div class="w-full max-w-sm rounded-xl border border-border bg-card p-8 shadow-sm">
		<div class="flex flex-col items-center gap-5 text-center">
			<div class="flex h-14 w-14 items-center justify-center rounded-xl bg-primary/5 border border-border p-3 text-foreground">
				{@html logoSvg}
			</div>
			<div class="flex flex-col items-center gap-1">
				<h1 class="text-base font-semibold text-foreground tracking-tight">Welcome to Pebble</h1>
				<p class="text-[13px] text-muted-foreground leading-relaxed">Create a password to secure your management dashboard.</p>
			</div>
			<form class="w-full space-y-3" onsubmit={(e) => { e.preventDefault(); setup(); }}>
				<input
					type="password"
					bind:value={setupPassword}
					placeholder="Password"
					class="w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
					disabled={setupLoading}
					autofocus
				/>
				<input
					type="password"
					bind:value={setupConfirm}
					placeholder="Confirm password"
					class="w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
					disabled={setupLoading}
				/>
				{#if setupError}
					<p class="text-xs text-destructive">{setupError}</p>
				{/if}
				<Button type="submit" class="w-full" disabled={setupLoading || !setupPassword || !setupConfirm}>
					{#if setupLoading}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					{/if}
					Set password
				</Button>
			</form>
			<p class="text-[10px] text-muted-foreground">Minimum 8 characters. You can also set <code class="bg-muted px-1 py-0.5 rounded">PEBBLE_PASSWORD</code> via environment variable.</p>
		</div>
	</div>
</div>

{:else if authRequired && !authenticated}
<!-- Login screen -->
<div class="flex min-h-screen flex-col items-center justify-center bg-background px-6 py-12">
	<div class="w-full max-w-sm rounded-xl border border-border bg-card p-8 shadow-sm">
		<div class="flex flex-col items-center gap-5 text-center">
			<div class="flex h-14 w-14 items-center justify-center rounded-xl bg-primary/5 border border-border p-3 text-foreground">
				{@html logoSvg}
			</div>
			<div class="flex flex-col items-center gap-1">
				<h1 class="text-base font-semibold text-foreground tracking-tight">Pebble</h1>
				<p class="text-[13px] text-muted-foreground leading-relaxed">Enter your password to continue.</p>
			</div>
			<form class="w-full space-y-3" onsubmit={(e) => { e.preventDefault(); login(); }}>
				<input
					type="password"
					bind:value={loginPassword}
					placeholder="Password"
					class="w-full rounded-md border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
					disabled={loginLoading}
					autofocus
				/>
				{#if loginError}
					<p class="text-xs text-destructive">{loginError}</p>
				{/if}
				<Button type="submit" class="w-full" disabled={loginLoading || !loginPassword}>
					{#if loginLoading}
						<Loader2 class="mr-2 h-4 w-4 animate-spin" />
					{/if}
					Sign in
				</Button>
			</form>
		</div>
	</div>
</div>

{:else}
<!-- Main app -->
<div class="flex h-screen w-screen flex-col overflow-hidden bg-background text-foreground">
<!-- Top bar -->
<header class="flex h-12 items-center gap-3 border-b border-border bg-background px-4 shrink-0">
	<div class="flex items-center gap-2">
		<span class="inline-block h-4 w-4 shrink-0">{@html logoSvg}</span>
		<span class="text-sm font-semibold tracking-tight">Pebble</span>
	</div>

	<div class="flex-1"></div>

	{#if hasPending}
	<span class="text-[11px] text-muted-foreground">{pendingCount} unsaved</span>
	<Tooltip content="Save & reload Caddy (⌘S)">
		<Button
			variant="ghost"
			size="icon"
			onclick={applyChanges}
			disabled={applyStatus === 'applying'}
			class="h-8 w-8"
		>
			{#if applyStatus === 'applying'}
				<Loader2 class="h-4 w-4 animate-spin text-green-500" />
			{:else if applyStatus === 'success'}
				<Check class="h-4 w-4 text-green-500" />
			{:else if applyStatus === 'error'}
				<AlertCircle class="h-4 w-4 text-destructive" />
			{:else}
				<Save class="h-4 w-4 text-green-500" />
			{/if}
		</Button>
	</Tooltip>
	{/if}

	<Tooltip content="GitHub Sync">
		<Button variant="ghost" size="icon" onclick={() => (settingsOpen = true)} class="h-8 w-8">
			<Github class="h-4 w-4" />
		</Button>
	</Tooltip>

	<Tooltip content={currentTheme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}>
		<Button variant="ghost" size="icon" onclick={toggleMode} class="h-8 w-8">
			{#if currentTheme === 'dark'}
				<Sun class="h-4 w-4" />
			{:else}
				<Moon class="h-4 w-4" />
			{/if}
		</Button>
	</Tooltip>

	{#if authRequired}
	<Tooltip content="Sign out">
		<Button variant="ghost" size="icon" onclick={logout} class="h-8 w-8">
			<LogOut class="h-4 w-4" />
		</Button>
	</Tooltip>
	{/if}
</header>

{#if caddyError}
<div class="flex items-center gap-2 border-b border-destructive/30 bg-destructive/10 px-4 py-2 text-xs text-destructive shrink-0">
	<AlertCircle class="h-3.5 w-3.5 shrink-0" />
	<span class="flex-1">Caddy reload failed: {caddyError}</span>
	<button onclick={() => (caddyError = null)} class="rounded p-0.5 hover:bg-destructive/20" aria-label="Dismiss"><X class="h-3 w-3" /></button>
</div>
{/if}

{#if applyError}
<div class="flex items-center gap-2 border-b border-amber-500/30 bg-amber-500/10 px-4 py-2 text-xs text-amber-600 dark:text-amber-400 shrink-0">
	<AlertCircle class="h-3.5 w-3.5 shrink-0" />
	<span class="flex-1">{applyError}</span>
	<button onclick={() => (applyError = null)} class="rounded p-0.5 hover:bg-amber-500/20" aria-label="Dismiss"><X class="h-3 w-3" /></button>
</div>
{/if}

<div class="flex flex-1 overflow-hidden">
<aside class="flex flex-col bg-sidebar shrink-0 overflow-hidden transition-all duration-200 {sidebarOpen ? 'w-64 border-r border-sidebar-border' : 'w-0'}">
	<div class="flex h-10 items-center gap-1 border-b border-sidebar-border px-3 shrink-0">
		<span class="flex-1 text-xs font-semibold uppercase tracking-wider text-muted-foreground">Files</span>
		<Tooltip content="New file">
			<button onclick={() => fileTreeRef?.startCreateAtRoot('file')}
				class="flex h-6 w-6 items-center justify-center rounded hover:bg-accent text-muted-foreground hover:text-foreground transition-colors" aria-label="New file">
				<FilePlus class="h-3.5 w-3.5" />
			</button>
		</Tooltip>
		<Tooltip content="New folder">
			<button onclick={() => fileTreeRef?.startCreateAtRoot('folder')}
				class="flex h-6 w-6 items-center justify-center rounded hover:bg-accent text-muted-foreground hover:text-foreground transition-colors" aria-label="New folder">
				<FolderPlus class="h-3.5 w-3.5" />
			</button>
		</Tooltip>
		<Tooltip content="Refresh">
			<button onclick={loadFiles}
				class="flex h-6 w-6 items-center justify-center rounded hover:bg-accent text-muted-foreground hover:text-foreground transition-colors" aria-label="Refresh">
				<RefreshCw class="h-3.5 w-3.5" />
			</button>
		</Tooltip>
	</div>

	<FileTree
		bind:this={fileTreeRef}
		tree={fileTree}
		{selectedFile}
		{expandedDirs}
		stagedChanges={stagedContent}
		{loadingFiles}
		onselect={openFile}
		ontoggle={toggleDir}
		onrename={handleRename}
		ondelete={handleDelete}
		oncreate={handleCreate}
		onmove={handleMove}
	/>
</aside>

<main class="flex flex-1 flex-col overflow-hidden">
	<div class="flex h-10 shrink-0 items-center gap-2 border-b border-border pl-2 pr-4 bg-muted/30">
		<Tooltip content={sidebarOpen ? 'Hide sidebar' : 'Show sidebar'}>
			<Button variant="ghost" size="icon" onclick={() => (sidebarOpen = !sidebarOpen)} class="h-6 w-6 shrink-0">
				<PanelLeft class="h-3.5 w-3.5" />
			</Button>
		</Tooltip>
		{#if selectedFile}
		<Separator orientation="vertical" class="h-4" />
		<File class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
		<span class="truncate text-xs font-mono text-muted-foreground">{selectedFile}{#if isDirty || stagedContent.has(selectedFile)}<span class="inline-block ml-1.5 h-1.5 w-1.5 rounded-full bg-foreground align-middle"></span>{/if}</span>
		{/if}
	</div>
	{#if loading}
	<div class="flex h-full items-center justify-center">
		<Loader2 class="h-6 w-6 animate-spin text-muted-foreground" />
	</div>
	{:else if selectedFile}
	<div class="flex-1 overflow-hidden">
		<Editor bind:value={editorContent} onChange={() => stageCurrentFile()} theme={currentTheme} lang={editorLang} />
	</div>
	{:else}
	<div class="flex h-full flex-col items-center justify-center gap-4 text-center">
		<div class="flex h-16 w-16 items-center justify-center rounded-2xl bg-muted/50">
			<File class="h-8 w-8 text-muted-foreground/60" />
		</div>
		<div>
			<p class="text-sm font-medium text-foreground">No file selected</p>
			<p class="mt-1 text-xs text-muted-foreground">Select a file from the sidebar to start editing</p>
		</div>
	</div>
	{/if}
</main>
</div>
</div>

<SettingsPanel bind:open={settingsOpen} onclose={() => (settingsOpen = false)} onSyncChange={(syncing) => { syncActive = syncing; if (syncing) loadFiles(); }} />
{/if}
