<script lang="ts">
import { onMount } from 'svelte';
import { toggleMode, mode } from 'mode-watcher';
import Editor from '$lib/components/Editor.svelte';
import { Button } from '$lib/components/ui/button';
import { Tooltip } from '$lib/components/ui/tooltip';
import { Separator } from '$lib/components/ui/separator';
import {
Sun,
Moon,
FolderOpen,
Plus,
Trash2,
RefreshCw,
ChevronRight,
File,
Loader2,
Check,
AlertCircle,
X,
FolderClosed,
Pencil,
RotateCcw,
PanelLeft
} from 'lucide-svelte';
import logoSvg from '$lib/assets/logo.svg?raw';

interface FileEntry {
name: string;
path: string;
isDirectory: boolean;
size?: number;
modifiedAt?: string;
}

interface FileNode {
name: string;
path: string;
isDirectory: boolean;
children?: FileNode[];
size?: number;
modifiedAt?: string;
}

// State
let files = $state<FileEntry[]>([]);
let fileTree = $state<FileNode[]>([]);
let selectedFile = $state<string | null>(null);
let editorContent = $state('');
let savedContent = $state('');
let loading = $state(false);
let saving = $state(false);
let saveStatus = $state<'idle' | 'saved' | 'error'>('idle');
let loadingFiles = $state(true);
let expandedDirs = $state<Set<string>>(new Set());
let creatingFile = $state(false);
let newFileInputValue = $state('');
let newFileError = $state('');
let renamingFile = $state<string | null>(null);
let renameValue = $state('');
let renameError = $state('');
let confirmDelete = $state<string | null>(null);
let caddyError = $state<string | null>(null);
let reloadStatus = $state<'idle' | 'reloading' | 'success' | 'error'>('idle');
let sidebarOpen = $state(true);

$effect(() => {
if (selectedFile && editorContent !== savedContent) {
const timer = setTimeout(() => saveFile(), 1000);
return () => clearTimeout(timer);
}
});

const isDirty = $derived(editorContent !== savedContent);
const currentTheme = $derived(mode.current === 'dark' ? 'dark' : 'light');

// Build tree from flat list
function buildTree(entries: FileEntry[]): FileNode[] {
const root: FileNode[] = [];
const nodeMap = new Map<string, FileNode>();

// Sort: directories first, then files
const sorted = [...entries].sort((a, b) => {
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

return root;
}

async function loadFiles() {
loadingFiles = true;
try {
const res = await fetch('/api/files');
const data = await res.json();
files = data.files;
fileTree = buildTree(data.files);
} catch (e) {
console.error('Failed to load files:', e);
} finally {
loadingFiles = false;
}
}

async function openFile(path: string) {
if (isDirty && selectedFile) {
await saveFile();
if (saveStatus === 'error') {
if (!confirm('Failed to save changes. Switch files and discard changes?')) return;
}
}
loading = true;
try {
const res = await fetch(`/api/files/${path}`);
if (!res.ok) throw new Error('Failed to load file');
const data = await res.json();
editorContent = data.content;
savedContent = data.content;
selectedFile = path;
saveStatus = 'idle';
} catch (e) {
console.error('Failed to open file:', e);
} finally {
loading = false;
}
}

async function saveFile() {
if (!selectedFile) return;
saving = true;
saveStatus = 'idle';
try {
const res = await fetch(`/api/files/${selectedFile}`, {
method: 'PUT',
headers: { 'Content-Type': 'application/json' },
body: JSON.stringify({ content: editorContent })
});
if (!res.ok) throw new Error('Save failed');
savedContent = editorContent;
saveStatus = 'saved';
setTimeout(() => (saveStatus = 'idle'), 2000);
} catch (e) {
saveStatus = 'error';
setTimeout(() => (saveStatus = 'idle'), 3000);
} finally {
saving = false;
}
}

async function createFile() {
newFileError = '';
const name = newFileInputValue.trim();
if (!name) {
newFileError = 'Please enter a file name.';
return;
}
try {
const res = await fetch(`/api/files/${name}`, {
method: 'POST',
headers: { 'Content-Type': 'application/json' },
body: JSON.stringify({ content: '' })
});
if (res.status === 409) {
newFileError = 'A file with that name already exists.';
return;
}
if (!res.ok) throw new Error('Create failed');
creatingFile = false;
newFileInputValue = '';
newFileError = '';
await loadFiles();
await openFile(name);
} catch (e) {
newFileError = 'Failed to create file.';
}
}

async function renameFile(oldPath: string, newName: string) {
const trimmed = newName.trim();
renameError = '';
if (!trimmed || trimmed === oldPath.split('/').pop()) {
renamingFile = null;
return;
}
const parts = oldPath.split('/');
parts[parts.length - 1] = trimmed;
const newPath = parts.join('/');
try {
const res = await fetch(`/api/files/${oldPath}`, {
method: 'PATCH',
headers: { 'Content-Type': 'application/json' },
body: JSON.stringify({ newPath })
});
if (!res.ok) throw new Error('Rename failed');
if (selectedFile === oldPath) {
selectedFile = newPath;
} else if (selectedFile?.startsWith(oldPath + '/')) {
selectedFile = newPath + selectedFile.slice(oldPath.length);
}
if (expandedDirs.has(oldPath)) {
expandedDirs.delete(oldPath);
expandedDirs.add(newPath);
expandedDirs = new Set(expandedDirs);
}
renamingFile = null;
await loadFiles();
} catch (e) {
console.error('Rename failed:', e);
renameError = 'Rename failed';
}
}

async function deleteFile(path: string) {
try {
const res = await fetch(`/api/files/${path}`, { method: 'DELETE' });
if (!res.ok) throw new Error('Delete failed');
if (selectedFile === path || selectedFile?.startsWith(path + '/')) {
selectedFile = null;
editorContent = '';
savedContent = '';
}
confirmDelete = null;
await loadFiles();
} catch (e) {
console.error('Delete failed:', e);
}
}

async function reloadCaddy() {
reloadStatus = 'reloading';
caddyError = null;
try {
const res = await fetch('/api/caddy/reload', { method: 'POST' });
const data = await res.json();
if (!res.ok || !data.success) {
reloadStatus = 'error';
caddyError = data.error ?? 'Reload failed';
setTimeout(() => (reloadStatus = 'idle'), 3000);
} else {
reloadStatus = 'success';
setTimeout(() => (reloadStatus = 'idle'), 2000);
}
} catch (e) {
reloadStatus = 'error';
caddyError = e instanceof Error ? e.message : 'Reload failed';
setTimeout(() => (reloadStatus = 'idle'), 3000);
}
}

function toggleDir(path: string) {
if (expandedDirs.has(path)) {
expandedDirs.delete(path);
} else {
expandedDirs.add(path);
}
expandedDirs = new Set(expandedDirs);
}

function handleKeydown(e: KeyboardEvent) {
if ((e.ctrlKey || e.metaKey) && e.key === 's') {
e.preventDefault();
if (selectedFile && isDirty) saveFile();
}
}

onMount(() => {
loadFiles();
window.addEventListener('keydown', handleKeydown);
if (window.innerWidth < 768) {
sidebarOpen = false;
}
return () => window.removeEventListener('keydown', handleKeydown);
});
</script>

<div class="flex h-screen w-screen flex-col overflow-hidden bg-background text-foreground">
<!-- Top bar -->
<header
class="flex h-12 items-center gap-3 border-b border-border bg-background px-4 shrink-0"
>
<div class="flex items-center gap-2">
<span class="inline-block h-4 w-4 shrink-0">{@html logoSvg}</span>
<span class="text-sm font-semibold tracking-tight">Pebble</span>
</div>

<div class="flex-1"></div>

<!-- Save error indicator -->
{#if selectedFile && saveStatus === 'error'}
<div class="flex items-center gap-2">
<span class="flex items-center gap-1 text-xs text-destructive">
<AlertCircle class="h-3 w-3" />
Save failed
</span>
</div>
{/if}

<!-- Reload Caddy button -->
<Tooltip content="Reload Caddy config">
<Button
variant="outline"
size="sm"
onclick={reloadCaddy}
disabled={reloadStatus === 'reloading'}
class="h-7 gap-1.5 text-xs"
>
{#if reloadStatus === 'reloading'}
<Loader2 class="h-3.5 w-3.5 animate-spin" />
{:else if reloadStatus === 'success'}
<Check class="h-3.5 w-3.5 text-green-500" />
{:else if reloadStatus === 'error'}
<AlertCircle class="h-3.5 w-3.5 text-destructive" />
{:else}
<RotateCcw class="h-3.5 w-3.5" />
{/if}
Reload
</Button>
</Tooltip>

<!-- Theme toggle -->
<Tooltip content={currentTheme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}>
<Button variant="ghost" size="icon" onclick={toggleMode} class="h-8 w-8">
{#if currentTheme === 'dark'}
<Sun class="h-4 w-4" />
{:else}
<Moon class="h-4 w-4" />
{/if}
</Button>
</Tooltip>
</header>

<!-- Caddy reload error banner -->
{#if caddyError}
<div class="flex items-center gap-2 border-b border-destructive/30 bg-destructive/10 px-4 py-2 text-xs text-destructive shrink-0">
<AlertCircle class="h-3.5 w-3.5 shrink-0" />
<span class="flex-1">Caddy config could not be applied: {caddyError}</span>
<button
onclick={() => (caddyError = null)}
class="flex items-center justify-center rounded p-0.5 hover:bg-destructive/20 transition-colors"
aria-label="Dismiss"
>
<X class="h-3 w-3" />
</button>
</div>
{/if}

<!-- Main content -->
<div class="flex flex-1 overflow-hidden">
<!-- Sidebar -->
<aside class="flex flex-col bg-sidebar shrink-0 overflow-hidden transition-all duration-200 {sidebarOpen ? 'w-64 border-r border-sidebar-border' : 'w-0'}">
<!-- Sidebar header -->
<div class="flex h-10 items-center gap-2 border-b border-sidebar-border px-3 shrink-0">
<span class="flex-1 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
Files
</span>
<Tooltip content="New file">
<button
onclick={() => {
creatingFile = true;
newFileInputValue = '';
newFileError = '';
}}
class="flex h-6 w-6 items-center justify-center rounded hover:bg-accent text-muted-foreground hover:text-foreground transition-colors"
aria-label="New file"
>
<Plus class="h-3.5 w-3.5" />
</button>
</Tooltip>
<Tooltip content="Refresh">
<button
onclick={loadFiles}
class="flex h-6 w-6 items-center justify-center rounded hover:bg-accent text-muted-foreground hover:text-foreground transition-colors"
aria-label="Refresh files"
>
<RefreshCw class="h-3.5 w-3.5" />
</button>
</Tooltip>
</div>

<!-- File tree -->
<div class="flex-1 overflow-y-auto py-1">
{#if loadingFiles}
<div class="flex items-center justify-center py-8">
<Loader2 class="h-4 w-4 animate-spin text-muted-foreground" />
</div>
{:else}
{#if creatingFile}
<div class="flex flex-col px-2 py-1">
<div class="flex items-center gap-1.5">
<File class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
<input
bind:value={newFileInputValue}
class="flex-1 min-w-0 bg-transparent text-xs focus:outline-none border-b border-primary py-0.5"
placeholder="filename"
autofocus
onkeydown={(e) => {
if (e.key === 'Enter') { e.preventDefault(); createFile(); }
if (e.key === 'Escape') { e.preventDefault(); creatingFile = false; newFileInputValue = ''; newFileError = ''; }
}}
onblur={() => {
if (newFileInputValue.trim()) createFile();
else { creatingFile = false; newFileError = ''; }
}}
/>
</div>
{#if newFileError}
<p class="mt-1 text-xs text-destructive pl-5">{newFileError}</p>
{/if}
</div>
{/if}
{#if fileTree.length === 0 && !creatingFile}
<div class="flex flex-col items-center gap-2 py-8 px-4 text-center">
<FolderOpen class="h-8 w-8 text-muted-foreground/50" />
<p class="text-xs text-muted-foreground">No files found</p>
<Button
variant="outline"
size="sm"
onclick={() => {
creatingFile = true;
newFileInputValue = '';
newFileError = '';
}}
class="h-7 text-xs mt-1"
>
<Plus class="h-3 w-3" />
New file
</Button>
</div>
{:else if fileTree.length > 0}
{#snippet renderNode(node: FileNode, depth: number)}
{#if node.isDirectory}
<div>
<div
class="group flex w-full items-center gap-1.5 py-1 text-sm hover:bg-accent/50 transition-colors text-sidebar-foreground cursor-pointer"
style={`padding-left: ${8 + depth * 12}px; padding-right: 8px;`}
onclick={() => { if (renamingFile !== node.path) toggleDir(node.path); }}
onkeydown={(e) => e.key === 'Enter' && renamingFile !== node.path && toggleDir(node.path)}
role="button"
tabindex="0"
aria-label={`Folder: ${node.name}`}
>
<ChevronRight
class={[
'h-3 w-3 shrink-0 text-muted-foreground transition-transform',
expandedDirs.has(node.path) ? 'rotate-90' : ''
].join(' ')}
/>
{#if expandedDirs.has(node.path)}
<FolderOpen class="h-3.5 w-3.5 shrink-0 text-primary/70" />
{:else}
<FolderClosed class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
{/if}
{#if renamingFile === node.path}
<input
bind:value={renameValue}
class="flex-1 min-w-0 bg-transparent text-xs focus:outline-none border-b border-primary h-4 leading-none"
onclick={(e) => e.stopPropagation()}
onkeydown={(e) => {
e.stopPropagation();
if (e.key === 'Enter') { e.preventDefault(); renameFile(node.path, renameValue); }
if (e.key === 'Escape') { e.preventDefault(); renamingFile = null; renameError = ''; }
}}
onblur={() => renameFile(node.path, renameValue)}
autofocus
/>
<button
class="flex shrink-0 items-center justify-center h-4 w-4 rounded text-muted-foreground hover:text-foreground transition-colors"
onmousedown={(e) => e.preventDefault()}
onclick={(e) => { e.stopPropagation(); renamingFile = null; renameError = ''; }}
aria-label="Cancel rename"
title="Cancel"
>
<X class="h-3 w-3" />
</button>
<span class="h-4 w-4 shrink-0"></span>
{:else if confirmDelete === node.path}
<span class="flex-1 truncate text-xs font-medium">{node.name}</span>
<div class="flex items-center gap-1" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()}>
<button
onclick={(e) => { e.stopPropagation(); deleteFile(node.path); }}
class="text-destructive hover:text-destructive/80 transition-colors"
aria-label="Confirm delete"
title="Confirm delete"
>
<Check class="h-3 w-3" />
</button>
<button
onclick={(e) => { e.stopPropagation(); confirmDelete = null; }}
class="text-muted-foreground hover:text-foreground transition-colors"
aria-label="Cancel delete"
title="Cancel"
>
<X class="h-3 w-3" />
</button>
</div>
{:else}
<span class="flex-1 truncate text-xs font-medium">{node.name}</span>
<button
class="opacity-0 group-hover:opacity-100 flex shrink-0 items-center justify-center h-4 w-4 rounded text-muted-foreground hover:text-foreground transition-opacity"
onclick={(e) => {
e.stopPropagation();
renamingFile = node.path;
renameValue = node.name;
renameError = '';
}}
aria-label="Rename folder"
title="Rename"
>
<Pencil class="h-3 w-3" />
</button>
<button
class="opacity-0 group-hover:opacity-100 flex shrink-0 items-center justify-center h-4 w-4 rounded text-muted-foreground hover:text-destructive transition-opacity"
onclick={(e) => {
e.stopPropagation();
confirmDelete = node.path;
}}
aria-label="Delete folder"
title="Delete"
>
<Trash2 class="h-3 w-3" />
</button>
{/if}
</div>
{#if expandedDirs.has(node.path) && node.children}
{#each node.children as child}
{@render renderNode(child, depth + 1)}
{/each}
{/if}
</div>
{:else}
<div
class={[
'group flex w-full items-center gap-1.5 py-1 text-sm transition-colors cursor-pointer',
selectedFile === node.path
? 'bg-accent text-accent-foreground'
: 'hover:bg-accent/50 text-sidebar-foreground'
].join(' ')}
style={`padding-left: ${8 + depth * 12}px; padding-right: 8px;`}
onclick={() => renamingFile !== node.path && openFile(node.path)}
onkeydown={(e) => e.key === 'Enter' && renamingFile !== node.path && openFile(node.path)}
role="button"
tabindex="0"
aria-label={`File: ${node.name}`}
>
<File class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
{#if renamingFile === node.path}
<input
bind:value={renameValue}
class="flex-1 min-w-0 bg-transparent text-xs focus:outline-none border-b border-primary h-4 leading-none"
onclick={(e) => e.stopPropagation()}
onkeydown={(e) => {
e.stopPropagation();
if (e.key === 'Enter') { e.preventDefault(); renameFile(node.path, renameValue); }
if (e.key === 'Escape') { e.preventDefault(); renamingFile = null; renameError = ''; }
}}
onblur={() => renameFile(node.path, renameValue)}
autofocus
/>
<button
class="flex shrink-0 items-center justify-center h-4 w-4 rounded text-muted-foreground hover:text-foreground transition-colors"
onclick={(e) => { e.stopPropagation(); renamingFile = null; renameError = ''; }}
aria-label="Cancel rename"
title="Cancel"
>
<X class="h-3 w-3" />
</button>
<span class="h-4 w-4 shrink-0"></span>
{:else}
<span class="flex-1 truncate text-xs">{node.name}</span>
{#if confirmDelete === node.path}
<div class="flex items-center gap-1" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()} role="none">
<button
onclick={() => deleteFile(node.path)}
class="text-destructive hover:text-destructive/80 transition-colors"
aria-label="Confirm delete"
title="Confirm delete"
>
<Check class="h-3 w-3" />
</button>
<button
onclick={() => (confirmDelete = null)}
class="text-muted-foreground hover:text-foreground transition-colors"
aria-label="Cancel delete"
title="Cancel"
>
<X class="h-3 w-3" />
</button>
</div>
{:else}
<button
class="opacity-0 group-hover:opacity-100 flex shrink-0 items-center justify-center h-4 w-4 rounded text-muted-foreground hover:text-foreground transition-opacity"
onclick={(e) => {
e.stopPropagation();
renamingFile = node.path;
renameValue = node.name;
renameError = '';
}}
aria-label="Rename file"
title="Rename"
>
<Pencil class="h-3 w-3" />
</button>
<button
class="opacity-0 group-hover:opacity-100 flex shrink-0 items-center justify-center h-4 w-4 rounded text-muted-foreground hover:text-destructive transition-opacity"
onclick={(e) => {
e.stopPropagation();
confirmDelete = node.path;
}}
aria-label="Delete file"
title="Delete"
>
<Trash2 class="h-3 w-3" />
</button>
{/if}
{/if}
</div>
{/if}
{/snippet}
{#each fileTree as node}
{@render renderNode(node, 0)}
{/each}
{/if}
{/if}
</div>
</aside>

<!-- Editor area -->
<main class="flex flex-1 flex-col overflow-hidden">
<!-- Editor header (always visible) -->
<div
class="flex h-10 shrink-0 items-center gap-2 border-b border-border pl-2 pr-4 bg-muted/30"
>
<Tooltip content={sidebarOpen ? 'Hide sidebar' : 'Show sidebar'}>
<Button variant="ghost" size="icon" onclick={() => (sidebarOpen = !sidebarOpen)} class="h-6 w-6 shrink-0">
<PanelLeft class="h-3.5 w-3.5" />
</Button>
</Tooltip>
{#if selectedFile}
<Separator orientation="vertical" class="h-4" />
<File class="h-3.5 w-3.5 text-muted-foreground shrink-0" />
<span class="truncate text-xs text-muted-foreground font-mono">{selectedFile}</span>
{/if}
</div>
{#if loading}
<div class="flex h-full items-center justify-center">
<Loader2 class="h-6 w-6 animate-spin text-muted-foreground" />
</div>
{:else if selectedFile}
<!-- Editor -->
<div class="flex-1 overflow-hidden">
<Editor
bind:value={editorContent}
theme={currentTheme}
/>
</div>
{:else}
<!-- Empty state -->
<div class="flex h-full flex-col items-center justify-center gap-4 text-center">
<div class="flex h-16 w-16 items-center justify-center rounded-2xl bg-muted/50">
<File class="h-8 w-8 text-muted-foreground/60" />
</div>
<div>
<p class="text-sm font-medium text-foreground">No file selected</p>
<p class="mt-1 text-xs text-muted-foreground">
Select a file from the sidebar to start editing
</p>
</div>
<Button
variant="outline"
size="sm"
onclick={() => {
creatingFile = true;
newFileInputValue = '';
newFileError = '';
}}
class="mt-2 gap-1.5"
>
<Plus class="h-3.5 w-3.5" />
New file
</Button>
</div>
{/if}
</main>
</div>
</div>
