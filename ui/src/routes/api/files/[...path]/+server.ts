import { json, error } from '@sveltejs/kit';
import { readFile, writeFile, mkdir, unlink, stat, rename, rm } from 'fs/promises';
import { join, dirname, resolve } from 'path';
import type { RequestHandler } from './$types';

const CADDYFILES_DIR = process.env.CADDYFILES_DIR ?? '/etc/caddy';
// Resolved base with trailing slash to prevent prefix-match bypass (e.g. /etc/caddy-extra)
const RESOLVED_BASE = resolve(CADDYFILES_DIR) + '/';

function resolveSafe(relativePath: string): string {
	// Prevent path traversal attacks
	const resolved = resolve(join(CADDYFILES_DIR, relativePath));
	if (resolved !== resolve(CADDYFILES_DIR) && !resolved.startsWith(RESOLVED_BASE)) {
		throw error(403, 'Access denied');
	}
	return resolved;
}

export const GET: RequestHandler = async ({ params }) => {
	const filePath = resolveSafe(params.path ?? '');
	try {
		const info = await stat(filePath);
		if (info.isDirectory()) {
			throw error(400, 'Path is a directory');
		}
		const content = await readFile(filePath, 'utf-8');
		return json({ content, path: params.path });
	} catch (e: unknown) {
		if (e && typeof e === 'object' && 'status' in e) throw e;
		throw error(404, 'File not found');
	}
};

export const PUT: RequestHandler = async ({ params, request }) => {
	const filePath = resolveSafe(params.path ?? '');
	const { content } = await request.json();
	if (typeof content !== 'string') {
		throw error(400, 'Invalid content');
	}
	try {
		await mkdir(dirname(filePath), { recursive: true });
		await writeFile(filePath, content, 'utf-8');
		return json({ success: true, path: params.path });
	} catch (e) {
		console.error('Write error:', e);
		throw error(500, 'Failed to write file');
	}
};

export const POST: RequestHandler = async ({ params, request }) => {
	const filePath = resolveSafe(params.path ?? '');
	const { content = '' } = await request.json().catch(() => ({ content: '' }));
	try {
		// Check if file already exists
		await stat(filePath);
		throw error(409, 'File already exists');
	} catch (e: unknown) {
		if (e && typeof e === 'object' && 'status' in e && (e as { status: number }).status === 409)
			throw e;
		// File doesn't exist, create it
		await mkdir(dirname(filePath), { recursive: true });
		await writeFile(filePath, content, 'utf-8');
		return json({ success: true, path: params.path }, { status: 201 });
	}
};

export const DELETE: RequestHandler = async ({ params }) => {
	const filePath = resolveSafe(params.path ?? '');
	try {
		const info = await stat(filePath);
		if (info.isDirectory()) {
			await rm(filePath, { recursive: true });
		} else {
			await unlink(filePath);
		}
		return json({ success: true, path: params.path });
	} catch (e: unknown) {
		if (e && typeof e === 'object' && 'status' in e) throw e;
		throw error(404, 'File not found');
	}
};

export const PATCH: RequestHandler = async ({ params, request }) => {
	const filePath = resolveSafe(params.path ?? '');
	let body: unknown;
	try {
		body = await request.json();
	} catch {
		throw error(400, 'Invalid JSON in request body');
	}
	const { newPath } = body as Record<string, unknown>;
	if (typeof newPath !== 'string' || !newPath.trim()) {
		throw error(400, 'Invalid newPath');
	}
	const newFilePath = resolveSafe(newPath.trim());
	try {
		await mkdir(dirname(newFilePath), { recursive: true });
		await rename(filePath, newFilePath);
		return json({ success: true, path: newPath.trim() });
	} catch (e) {
		console.error('Rename error:', e);
		throw error(500, 'Failed to rename file');
	}
};
