<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { EditorView, keymap, lineNumbers, highlightActiveLineGutter, highlightSpecialChars, drawSelection, dropCursor, rectangularSelection, crosshairCursor, highlightActiveLine } from '@codemirror/view';
	import { EditorState, Compartment } from '@codemirror/state';
	import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
	import { foldGutter, indentOnInput, syntaxHighlighting, defaultHighlightStyle, bracketMatching, foldKeymap } from '@codemirror/language';
	import { closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete';
	import { oneDark } from '@codemirror/theme-one-dark';
	import type { Extension } from '@codemirror/state';

	interface Props {
		value: string;
		onChange?: (value: string) => void;
		theme?: 'light' | 'dark';
		readonly?: boolean;
		lang?: Extension | null;
	}

	let { value = $bindable(''), onChange, theme = 'light', readonly = false, lang = null }: Props = $props();

	let container: HTMLDivElement;
	let view: EditorView | null = null;
	const themeCompartment = new Compartment();
	const readonlyCompartment = new Compartment();
	const langCompartment = new Compartment();

	function getThemeExtension(t: 'light' | 'dark') {
		return t === 'dark' ? oneDark : EditorView.baseTheme({});
	}

	onMount(() => {
		const state = EditorState.create({
			doc: value,
			extensions: [
				lineNumbers(),
				highlightActiveLineGutter(),
				highlightSpecialChars(),
				history(),
				foldGutter(),
				drawSelection(),
				dropCursor(),
				EditorState.allowMultipleSelections.of(true),
				indentOnInput(),
				syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
				bracketMatching(),
				closeBrackets(),
				rectangularSelection(),
				crosshairCursor(),
				highlightActiveLine(),
				keymap.of([
					...closeBracketsKeymap,
					...defaultKeymap,
					...historyKeymap,
					...foldKeymap,
					indentWithTab
				]),
				themeCompartment.of(getThemeExtension(theme)),
				readonlyCompartment.of(EditorState.readOnly.of(readonly)),
				langCompartment.of(lang ?? []),
				EditorView.updateListener.of((update) => {
					if (update.docChanged) {
						const newValue = update.state.doc.toString();
						value = newValue;
						onChange?.(newValue);
					}
				}),
				EditorView.theme({
					'&': {
						height: '100%',
						fontSize: '13px',
						fontFamily: '"Geist Mono", "Fira Code", "JetBrains Mono", Menlo, Consolas, monospace'
					},
					'.cm-scroller': {
						overflow: 'auto',
						height: '100%'
					},
					'.cm-content': {
						padding: '8px 0',
						minHeight: '100%'
					}
				})
			]
		});

		view = new EditorView({
			state,
			parent: container
		});
	});

	onDestroy(() => {
		view?.destroy();
	});

	$effect(() => {
		if (!view) return;
		// Update theme
		view.dispatch({
			effects: themeCompartment.reconfigure(getThemeExtension(theme))
		});
	});

	$effect(() => {
		if (!view) return;
		// Update content if it changed externally
		const currentValue = view.state.doc.toString();
		if (currentValue !== value) {
			view.dispatch({
				changes: { from: 0, to: currentValue.length, insert: value }
			});
		}
	});

	$effect(() => {
		if (!view) return;
		view.dispatch({
			effects: readonlyCompartment.reconfigure(EditorState.readOnly.of(readonly))
		});
	});

	$effect(() => {
		if (!view) return;
		view.dispatch({
			effects: langCompartment.reconfigure(lang ?? [])
		});
	});
</script>

<div bind:this={container} class="h-full w-full overflow-hidden"></div>
