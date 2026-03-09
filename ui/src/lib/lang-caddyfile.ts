/**
 * CodeMirror language support for Caddyfile syntax.
 * Provides syntax highlighting for Caddy v2 config files.
 */
import {
	StreamLanguage,
	LanguageSupport,
	syntaxHighlighting,
	HighlightStyle
} from '@codemirror/language';
import { tags } from '@lezer/highlight';

// ── Tokenizer ──

interface CaddyState {
	/** Nesting depth inside {} blocks */
	depth: number;
	/** Whether we're at the start of a line (for directive detection) */
	lineStart: boolean;
	/** Whether this line looks like a site address (top-level, no directive keyword) */
	inSiteBlock: boolean;
}

const caddyfileStreamParser = {
	name: 'caddyfile',

	startState(): CaddyState {
		return { depth: 0, lineStart: true, inSiteBlock: false };
	},

	token(stream: any, state: CaddyState): string | null {
		// Whitespace
		if (stream.eatSpace()) {
			return null;
		}

		// Comments: # to end of line
		if (stream.match('#')) {
			stream.skipToEnd();
			return 'comment';
		}

		// Opening brace
		if (stream.match('{')) {
			state.depth++;
			state.lineStart = true;
			return 'brace';
		}

		// Closing brace
		if (stream.match('}')) {
			state.depth = Math.max(0, state.depth - 1);
			state.lineStart = true;
			return 'brace';
		}

		// Strings (double-quoted)
		if (stream.match('"')) {
			while (!stream.eol()) {
				const ch = stream.next();
				if (ch === '"') break;
				if (ch === '\\') stream.next(); // skip escaped char
			}
			return 'string';
		}

		// Backtick strings
		if (stream.match('`')) {
			while (!stream.eol()) {
				if (stream.next() === '`') break;
			}
			return 'string';
		}

		// Placeholders: {env.VAR}, {args[0]}, {http.request.*}, etc.
		if (stream.match(/^\{[a-zA-Z][a-zA-Z0-9_.[\]]*\}/)) {
			return 'variableName.special';
		}

		// At line start, detect what kind of token this is
		if (state.lineStart) {
			state.lineStart = false;

			// Depth 0: site addresses (hostnames, :port, *, etc.)
			if (state.depth === 0) {
				if (stream.match(/^[a-zA-Z0-9*][\w.*:/-]*/)) {
					return 'typeName';
				}
			}

			// Depth 1+: directives
			if (state.depth >= 1) {
				// Matchers: @name
				if (stream.match(/^@[\w-]+/)) {
					return 'labelName';
				}

				// Named matchers used inline: @name
				// Directives
				if (stream.match(/^[a-z][\w_-]*/)) {
					const word = stream.current();
					if (DIRECTIVES.has(word)) {
						return 'keyword';
					}
					if (SUBDIRECTIVES.has(word)) {
						return 'keyword';
					}
					// Could be a subdirective or unknown directive
					return 'keyword';
				}
			}
		}

		// Matcher tokens inline: @name
		if (stream.match(/^@[\w-]+/)) {
			return 'labelName';
		}

		// Numbers (ports, status codes, durations)
		if (stream.match(/^\d[\d.smhd]*/)) {
			return 'number';
		}

		// Paths starting with /
		if (stream.match(/^\/[\w/.{}\-*]*/)) {
			return 'string.special';
		}

		// URLs
		if (stream.match(/^https?:\/\/[\w./:@%-]*/)) {
			return 'url';
		}

		// Wildcard *
		if (stream.match('*')) {
			return 'operator';
		}

		// Anything else — consume one word
		if (stream.match(/^[\w.:-]+/)) {
			const word = stream.current();
			// Check for known values
			if (word === 'true' || word === 'false' || word === 'on' || word === 'off') {
				return 'bool';
			}
			if (DIRECTIVES.has(word) || SUBDIRECTIVES.has(word)) {
				return 'keyword';
			}
			return null;
		}

		// Skip unknown chars
		stream.next();
		return null;
	},

	blankLine(state: CaddyState) {
		state.lineStart = true;
	},

	indent(state: CaddyState, _textAfter: string): number {
		return state.depth * 4;
	}
};

// ── Directive sets (used by tokenizer for keyword detection) ──

const DIRECTIVES = new Set([
	'abort', 'acme_server', 'basic_auth', 'bind', 'encode', 'error',
	'file_server', 'forward_auth', 'handle', 'handle_errors', 'handle_path',
	'header', 'import', 'invoke', 'log', 'map', 'method', 'metrics',
	'php_fastcgi', 'redir', 'request_body', 'request_header',
	'respond', 'reverse_proxy', 'rewrite', 'root', 'route', 'templates',
	'tls', 'tracing', 'try_files', 'uri', 'vars',
	'admin', 'auto_https', 'cert_issuer', 'debug', 'default_bind',
	'default_sni', 'email', 'grace_period', 'local_certs', 'ocsp_stapling',
	'order', 'persist_config', 'servers', 'skip_install_trust', 'storage'
]);

const SUBDIRECTIVES = new Set([
	'to', 'lb_policy', 'lb_retries', 'lb_try_duration', 'lb_try_interval',
	'health_uri', 'health_port', 'health_interval', 'health_timeout',
	'health_status', 'health_body', 'health_headers', 'fail_duration',
	'max_fails', 'unhealthy_status', 'unhealthy_latency',
	'unhealthy_request_count', 'flush_interval', 'buffer_requests',
	'buffer_responses', 'max_buffer_size', 'trusted_proxies',
	'header_up', 'header_down', 'transport',
	'protocols', 'ciphers', 'curves', 'alpn', 'ca', 'ca_root',
	'client_auth', 'dns', 'issuer', 'key_type', 'resolvers',
	'gzip', 'zstd', 'minimum_length', 'match',
	'output', 'format', 'level', 'include', 'exclude',
	'browse', 'precompressed', 'index', 'hide', 'pass_thru', 'status',
	'defer', 'delete', 'replace', 'set',
	'bcrypt', 'realm', 'max_size',
	'path', 'host', 'method', 'protocol', 'query', 'expression',
	'remote_ip', 'not', 'header_regexp', 'path_regexp'
]);

// ── Highlight style ──

const caddyHighlightStyle = HighlightStyle.define([
	{ tag: tags.keyword, color: '#c678dd' },
	{ tag: tags.typeName, color: '#e5c07b', fontWeight: 'bold' },
	{ tag: tags.comment, color: '#7f848e', fontStyle: 'italic' },
	{ tag: tags.string, color: '#98c379' },
	{ tag: tags.special(tags.string), color: '#61afef' },
	{ tag: tags.number, color: '#d19a66' },
	{ tag: tags.bool, color: '#d19a66' },
	{ tag: tags.labelName, color: '#56b6c2' },
	{ tag: tags.brace, color: '#abb2bf' },
	{ tag: tags.operator, color: '#56b6c2' },
	{ tag: tags.url, color: '#98c379', textDecoration: 'underline' },
	{ tag: tags.special(tags.variableName), color: '#e06c75' }
]);

// ── Language definition ──

const caddyfileLang = StreamLanguage.define(caddyfileStreamParser);

/**
 * Returns CodeMirror extension for Caddyfile syntax highlighting.
 */
export function caddyfile() {
	return new LanguageSupport(caddyfileLang, [
		syntaxHighlighting(caddyHighlightStyle)
	]);
}

