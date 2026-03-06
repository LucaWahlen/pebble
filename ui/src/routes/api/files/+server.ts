import { json } from '@sveltejs/kit';
import { readdir, stat } from 'fs/promises';
import { join } from 'path';

const CADDYFILES_DIR = process.env.CADDYFILES_DIR ?? '/etc/caddy';

interface FileEntry {
	name: string;
	path: string;
	isDirectory: boolean;
	size?: number;
	modifiedAt?: string;
}

async function listDir(dir: string, base: string): Promise<FileEntry[]> {
	const entries: FileEntry[] = [];
	try {
		const items = await readdir(dir);
		for (const item of items) {
			const fullPath = join(dir, item);
			const relativePath = join(base, item);
			try {
				const info = await stat(fullPath);
				entries.push({
					name: item,
					path: relativePath,
					isDirectory: info.isDirectory(),
					size: info.isDirectory() ? undefined : info.size,
					modifiedAt: info.mtime.toISOString()
				});
				if (info.isDirectory()) {
					const children = await listDir(fullPath, relativePath);
					entries.push(...children);
				}
			} catch {
				// skip inaccessible entries
			}
		}
	} catch {
		// ignore
	}
	return entries;
}

export async function GET() {
	const files = await listDir(CADDYFILES_DIR, '');
	return json({ files, root: CADDYFILES_DIR });
}
