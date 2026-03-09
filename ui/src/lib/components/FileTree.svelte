<script lang="ts">
	import type { FileNode } from '$lib/types';
	import {
		ChevronRight,
		File,
		FolderOpen,
		FolderClosed,
		Loader2,
		X,
		FilePlus,
		FolderPlus,
		Pencil,
		Trash2
	} from 'lucide-svelte';

	interface Props {
		tree: FileNode[];
		selectedFile: string | null;
		expandedDirs: Set<string>;
		stagedChanges: Map<string, string>;
		loadingFiles: boolean;
		onselect: (path: string) => void;
		ontoggle: (path: string) => void;
		onrename: (oldPath: string, newName: string) => void;
		ondelete: (path: string) => void;
		oncreate: (name: string, type: 'file' | 'folder', parentDir: string) => void;
		onmove: (sourcePath: string, targetDir: string) => void;
	}

	let {
		tree,
		selectedFile,
		expandedDirs,
		stagedChanges,
		loadingFiles,
		onselect,
		ontoggle,
		onrename,
		ondelete,
		oncreate,
		onmove
	}: Props = $props();

	// Internal state
	let renamingFile = $state<string | null>(null);
	let renameValue = $state('');
	let creatingIn = $state<{ dir: string; type: 'file' | 'folder' } | null>(null);
	let createValue = $state('');
	let createError = $state('');

	// Drag and drop
	let draggedPath = $state<string | null>(null);
	let dropTarget = $state<string | null>(null);

	// Context menu
	let contextMenu = $state<{ x: number; y: number; node: FileNode | null } | null>(null);

	// Delete confirmation
	let confirmDeletePath = $state<string | null>(null);

	function startRename(node: FileNode) {
		renamingFile = node.path;
		renameValue = node.name;
		contextMenu = null;
	}

	function commitRename(oldPath: string) {
		const trimmed = renameValue.trim();
		if (!trimmed || trimmed === oldPath.split('/').pop()) {
			renamingFile = null;
			return;
		}
		onrename(oldPath, trimmed);
		renamingFile = null;
	}

	export function startCreateAtRoot(type: 'file' | 'folder') {
		startCreate(type, '');
	}

	function startCreate(type: 'file' | 'folder', parentDir: string) {
		creatingIn = { dir: parentDir, type };
		createValue = '';
		createError = '';
		contextMenu = null;
		if (parentDir && !expandedDirs.has(parentDir)) {
			ontoggle(parentDir);
		}
	}

	function commitCreate() {
		const name = createValue.trim();
		if (!name) { createError = 'Enter a name.'; return; }
		const fullPath = creatingIn!.dir ? `${creatingIn!.dir}/${name}` : name;
		oncreate(fullPath, creatingIn!.type, creatingIn!.dir);
		creatingIn = null;
		createValue = '';
		createError = '';
	}

	function cancelCreate() {
		creatingIn = null;
		createValue = '';
		createError = '';
	}

	function requestDelete(path: string) {
		confirmDeletePath = path;
		contextMenu = null;
	}

	function confirmDelete() {
		if (confirmDeletePath) {
			ondelete(confirmDeletePath);
			confirmDeletePath = null;
		}
	}

	function cancelDelete() {
		confirmDeletePath = null;
	}

	function openContextMenu(e: MouseEvent, node: FileNode | null) {
		e.preventDefault();
		e.stopPropagation();
		contextMenu = { x: e.clientX, y: e.clientY, node };
	}

	function closeContextMenu() {
		contextMenu = null;
	}

	// Drag and drop handlers
	function handleDragStart(e: DragEvent, path: string) {
		draggedPath = path;
		if (e.dataTransfer) {
			e.dataTransfer.effectAllowed = 'move';
			e.dataTransfer.setData('text/plain', path);
		}
	}

	function handleDragOver(e: DragEvent, targetPath: string, isDir: boolean) {
		if (!draggedPath || draggedPath === targetPath) return;
		const draggedParent = draggedPath.includes('/') ? draggedPath.split('/').slice(0, -1).join('/') : '';
		const dest = isDir ? targetPath : (targetPath.includes('/') ? targetPath.split('/').slice(0, -1).join('/') : '');
		if (dest === draggedParent) return;
		// Prevent dropping a folder into itself or its own children
		if (dest === draggedPath || dest.startsWith(draggedPath + '/')) return;
		e.preventDefault();
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
		dropTarget = isDir ? targetPath : dest;
	}

	function handleDragLeave() { dropTarget = null; }

	async function handleDrop(e: DragEvent, targetPath: string, isDir: boolean) {
		e.preventDefault();
		dropTarget = null;
		if (!draggedPath || draggedPath === targetPath) { draggedPath = null; return; }
		const dest = isDir ? targetPath : (targetPath.includes('/') ? targetPath.split('/').slice(0, -1).join('/') : '');
		if (dest === draggedPath || dest.startsWith(draggedPath + '/')) { draggedPath = null; return; }
		onmove(draggedPath, dest);
		draggedPath = null;
	}

	function handleDragEnd() { draggedPath = null; dropTarget = null; }

	function handleRootDragOver(e: DragEvent) {
		if (!draggedPath) return;
		const draggedParent = draggedPath.includes('/') ? draggedPath.split('/').slice(0, -1).join('/') : '';
		if (draggedParent === '') return;
		e.preventDefault();
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
		dropTarget = '__root__';
	}

	async function handleRootDrop(e: DragEvent) {
		e.preventDefault();
		dropTarget = null;
		if (!draggedPath) return;
		onmove(draggedPath, '');
		draggedPath = null;
	}

	function isCreatingInDir(dirPath: string): boolean {
		return creatingIn !== null && creatingIn.dir === dirPath;
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class="flex-1 overflow-y-auto select-none"
	ondragover={(e) => handleRootDragOver(e)}
	ondrop={(e) => handleRootDrop(e)}
	ondragleave={() => { if (dropTarget === '__root__') dropTarget = null; }}
	oncontextmenu={(e) => openContextMenu(e, null)}
	role="tree"
>
	{#if loadingFiles}
		<div class="flex items-center justify-center py-8">
			<Loader2 class="h-4 w-4 animate-spin text-muted-foreground" />
		</div>
	{:else if tree.length === 0 && !creatingIn}
		<div class="flex flex-col items-center gap-2 py-8 px-4 text-center">
			<FolderOpen class="h-8 w-8 text-muted-foreground/50" />
			<p class="text-xs text-muted-foreground">No files yet</p>
			<p class="text-[10px] text-muted-foreground/60">Right-click to create</p>
		</div>
	{:else}
		{#if isCreatingInDir('')}
			{@render createInput(0)}
		{/if}

		{#each tree as node}
			{@render renderNode(node, 0)}
		{/each}
	{/if}
</div>

<!-- Context menu -->
{#if contextMenu}
	<button
		class="fixed inset-0 z-[100]"
		onclick={closeContextMenu}
		oncontextmenu={(e) => { e.preventDefault(); closeContextMenu(); }}
		aria-label="Close menu"
	></button>
	<div
		class="fixed z-[101] min-w-[160px] rounded-lg border border-border bg-popover p-1 text-popover-foreground shadow-lg"
		style="left: {contextMenu.x}px; top: {contextMenu.y}px;"
	>
		{#if contextMenu.node?.isDirectory}
			<!-- Directory context menu -->
			<button class="context-item" onclick={() => startCreate('file', contextMenu?.node?.path ?? '')}>
				<FilePlus class="h-3.5 w-3.5" /> New File
			</button>
			<button class="context-item" onclick={() => startCreate('folder', contextMenu?.node?.path ?? '')}>
				<FolderPlus class="h-3.5 w-3.5" /> New Folder
			</button>
			<div class="my-1 h-px bg-border"></div>
			<button class="context-item" onclick={() => contextMenu?.node && startRename(contextMenu.node)}>
				<Pencil class="h-3.5 w-3.5" /> Rename
			</button>
			<div class="my-1 h-px bg-border"></div>
			<button class="context-item context-item-danger" onclick={() => contextMenu?.node && requestDelete(contextMenu.node.path)}>
				<Trash2 class="h-3.5 w-3.5" /> Delete
			</button>
		{:else if contextMenu.node}
			<!-- File context menu -->
			<button class="context-item" onclick={() => contextMenu?.node && startRename(contextMenu.node)}>
				<Pencil class="h-3.5 w-3.5" /> Rename
			</button>
			<div class="my-1 h-px bg-border"></div>
			<button class="context-item context-item-danger" onclick={() => contextMenu?.node && requestDelete(contextMenu.node.path)}>
				<Trash2 class="h-3.5 w-3.5" /> Delete
			</button>
		{:else}
			<!-- Root (empty area) context menu -->
			<button class="context-item" onclick={() => startCreate('file', '')}>
				<FilePlus class="h-3.5 w-3.5" /> New File
			</button>
			<button class="context-item" onclick={() => startCreate('folder', '')}>
				<FolderPlus class="h-3.5 w-3.5" /> New Folder
			</button>
		{/if}
	</div>
{/if}

<!-- Delete confirmation dialog -->
{#if confirmDeletePath}
	<button
		class="fixed inset-0 z-[200] bg-black/40 backdrop-blur-sm"
		onclick={cancelDelete}
		aria-label="Cancel delete"
	></button>
	<div class="fixed left-1/2 top-1/2 z-[201] w-72 -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-popover p-4 shadow-xl">
		<p class="text-sm font-medium text-popover-foreground">Delete "{confirmDeletePath.split('/').pop()}"?</p>
		<p class="mt-1 text-xs text-muted-foreground">This action cannot be undone.</p>
		<div class="mt-4 flex justify-end gap-2">
			<button
				onclick={cancelDelete}
				class="rounded-md px-3 py-1.5 text-xs font-medium text-muted-foreground hover:bg-accent transition-colors"
			>Cancel</button>
			<button
				onclick={confirmDelete}
				class="rounded-md bg-destructive px-3 py-1.5 text-xs font-medium text-destructive-foreground hover:bg-destructive/90 transition-colors"
			>Delete</button>
		</div>
	</div>
{/if}

<!-- Snippets -->
{#snippet createInput(depth: number)}
	<div class="flex flex-col" style="padding-left: {8 + depth * 16}px; padding-right: 8px;">
		<div class="flex items-center gap-1.5 h-7">
			{#if creatingIn?.type === 'folder'}
				<FolderClosed class="h-4 w-4 shrink-0 text-muted-foreground" />
			{:else}
				<File class="h-4 w-4 shrink-0 text-muted-foreground" />
			{/if}
			<!-- svelte-ignore a11y_autofocus -->
			<input
				bind:value={createValue}
				class="flex-1 min-w-0 rounded bg-background px-1.5 py-0.5 text-[13px] text-foreground border border-ring focus:outline-none"
				placeholder={creatingIn?.type === 'folder' ? 'folder name' : 'filename'}
				autofocus
				onkeydown={(e) => {
					if (e.key === 'Enter') { e.preventDefault(); commitCreate(); }
					if (e.key === 'Escape') { e.preventDefault(); cancelCreate(); }
				}}
				onblur={() => { if (createValue.trim()) commitCreate(); else cancelCreate(); }}
			/>
			<button class="shrink-0 text-muted-foreground hover:text-foreground" onmousedown={(e) => e.preventDefault()} onclick={cancelCreate}>
				<X class="h-3 w-3" />
			</button>
		</div>
		{#if createError}
			<p class="mt-0.5 text-[11px] text-destructive" style="padding-left: 22px;">{createError}</p>
		{/if}
	</div>
{/snippet}

{#snippet renderNode(node: FileNode, depth: number)}
	{#if node.isDirectory}
		<!-- Directory -->
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			class="group flex w-full items-center gap-1.5 h-7 text-[13px] transition-colors text-sidebar-foreground cursor-pointer
				{dropTarget === node.path ? 'bg-primary/15' : 'hover:bg-accent/50'}"
			style="padding-left: {8 + depth * 16}px; padding-right: 4px;"
			onclick={() => { if (renamingFile !== node.path) ontoggle(node.path); }}
			onkeydown={(e) => { if (e.key === 'Enter' && renamingFile !== node.path) ontoggle(node.path); }}
			oncontextmenu={(e) => openContextMenu(e, node)}
			draggable={renamingFile !== node.path}
			ondragstart={(e) => handleDragStart(e, node.path)}
			ondragend={handleDragEnd}
			ondragover={(e) => handleDragOver(e, node.path, true)}
			ondragleave={handleDragLeave}
			ondrop={(e) => handleDrop(e, node.path, true)}
			role="treeitem"
			tabindex="0"
			aria-selected={false}
			aria-expanded={expandedDirs.has(node.path)}
		>
			<ChevronRight class="h-3 w-3 shrink-0 text-muted-foreground transition-transform duration-150
				{expandedDirs.has(node.path) ? 'rotate-90' : ''}" />
			{#if expandedDirs.has(node.path)}
				<FolderOpen class="h-4 w-4 shrink-0 text-primary/70" />
			{:else}
				<FolderClosed class="h-4 w-4 shrink-0 text-muted-foreground" />
			{/if}
			{#if renamingFile === node.path}
				<!-- svelte-ignore a11y_autofocus -->
				<input
					bind:value={renameValue}
					class="flex-1 min-w-0 rounded bg-background px-1.5 py-0.5 text-[13px] text-foreground border border-ring focus:outline-none"
					onclick={(e) => e.stopPropagation()}
					onkeydown={(e) => { e.stopPropagation(); if (e.key === 'Enter') { e.preventDefault(); commitRename(node.path); } if (e.key === 'Escape') { e.preventDefault(); renamingFile = null; } }}
					onblur={() => commitRename(node.path)}
					autofocus
				/>
			{:else}
				<span class="flex-1 truncate font-medium">{node.name}</span>
				<!-- Hover actions (always visible on mobile) -->
				<button class="opacity-0 group-hover:opacity-100 max-md:opacity-100 shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground transition-opacity"
					onclick={(e) => { e.stopPropagation(); renamingFile = node.path; renameValue = node.name; }} title="Rename">
					<Pencil class="h-3 w-3" />
				</button>
				<button class="opacity-0 group-hover:opacity-100 max-md:opacity-100 shrink-0 rounded p-0.5 text-muted-foreground hover:text-destructive transition-opacity"
					onclick={(e) => { e.stopPropagation(); requestDelete(node.path); }} title="Delete">
					<Trash2 class="h-3 w-3" />
				</button>
			{/if}
		</div>
		{#if expandedDirs.has(node.path) && node.children}
			{#if isCreatingInDir(node.path)}
				{@render createInput(depth + 1)}
			{/if}
			{#each node.children as child}
				{@render renderNode(child, depth + 1)}
			{/each}
			{#if node.children.length === 0 && !isCreatingInDir(node.path)}
				<div class="h-0"></div>
			{/if}
		{/if}
	{:else}
		<!-- File -->
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			class="group flex w-full items-center gap-1.5 h-7 text-[13px] transition-colors cursor-pointer
				{selectedFile === node.path ? 'bg-accent text-accent-foreground' : dropTarget === node.path ? 'bg-primary/10' : 'hover:bg-accent/50 text-sidebar-foreground'}"
			style="padding-left: {8 + depth * 16}px; padding-right: 4px;"
			onclick={() => renamingFile !== node.path && onselect(node.path)}
			onkeydown={(e) => { if (e.key === 'Enter' && renamingFile !== node.path) onselect(node.path); }}
			oncontextmenu={(e) => openContextMenu(e, node)}
			draggable={renamingFile !== node.path}
			ondragstart={(e) => handleDragStart(e, node.path)}
			ondragend={handleDragEnd}
			ondragover={(e) => handleDragOver(e, node.path, false)}
			ondragleave={handleDragLeave}
			ondrop={(e) => handleDrop(e, node.path, false)}
			role="treeitem"
			tabindex="0"
			aria-selected={selectedFile === node.path}
		>
			<File class="h-4 w-4 shrink-0 text-muted-foreground" />
			{#if renamingFile === node.path}
				<!-- svelte-ignore a11y_autofocus -->
				<input
					bind:value={renameValue}
					class="flex-1 min-w-0 rounded bg-background px-1.5 py-0.5 text-[13px] text-foreground border border-ring focus:outline-none"
					onclick={(e) => e.stopPropagation()}
					onkeydown={(e) => { e.stopPropagation(); if (e.key === 'Enter') { e.preventDefault(); commitRename(node.path); } if (e.key === 'Escape') { e.preventDefault(); renamingFile = null; } }}
					onblur={() => commitRename(node.path)}
					autofocus
				/>
			{:else}
				<span class="flex-1 truncate">
					{node.name}
					{#if stagedChanges.has(node.path)}
						<span class="inline-block ml-1.5 h-1.5 w-1.5 rounded-full bg-foreground align-middle"></span>
					{/if}
				</span>
				<!-- Hover actions (always visible on mobile) -->
				<button class="shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground transition-opacity
					opacity-0 group-hover:opacity-100 max-md:opacity-100"
					onclick={(e) => { e.stopPropagation(); renamingFile = node.path; renameValue = node.name; }} title="Rename">
					<Pencil class="h-3 w-3" />
				</button>
				<button class="shrink-0 rounded p-0.5 text-muted-foreground hover:text-destructive transition-opacity
					opacity-0 group-hover:opacity-100 max-md:opacity-100"
					onclick={(e) => { e.stopPropagation(); requestDelete(node.path); }} title="Delete">
					<Trash2 class="h-3 w-3" />
				</button>
			{/if}
		</div>
	{/if}
{/snippet}

<style>
	.context-item {
		display: flex;
		width: 100%;
		align-items: center;
		gap: 0.5rem;
		border-radius: 0.375rem;
		padding: 0.375rem 0.5rem;
		font-size: 0.75rem;
		line-height: 1rem;
		color: hsl(var(--popover-foreground));
		transition: background-color 0.1s;
		cursor: pointer;
		border: none;
		background: none;
		text-align: left;
	}
	.context-item:hover {
		background-color: hsl(var(--accent));
		color: hsl(var(--accent-foreground));
	}
	.context-item-danger:hover {
		background-color: hsl(var(--destructive) / 0.1);
		color: hsl(var(--destructive));
	}
</style>




