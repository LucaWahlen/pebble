import { json } from '@sveltejs/kit';
import { execFile } from 'child_process';
import { promisify } from 'util';
import { join } from 'path';

const execFileAsync = promisify(execFile);
const CADDYFILES_DIR = process.env.CADDYFILES_DIR ?? '/etc/caddy';

export async function POST() {
	const configFile = join(CADDYFILES_DIR, 'Caddyfile');
	try {
		await execFileAsync('caddy', ['reload', '--config', configFile, '--adapter', 'caddyfile']);
		return json({ success: true });
	} catch (e: unknown) {
		// Caddy binary not available (e.g. development environment without Caddy installed)
		if ((e as { code?: string }).code === 'ENOENT') {
			console.warn('caddy binary not found; skipping config reload');
			return json({ success: true, skipped: true, reason: 'caddy binary not found' });
		}
		const stderr = (e as { stderr?: string }).stderr?.trim();
		const message = stderr || (e instanceof Error ? e.message : String(e));
		console.error('Caddy reload failed:', message);
		return json({ success: false, error: message }, { status: 500 });
	}
}
