import adapter from '@sveltejs/adapter-static';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	kit: {
		adapter: adapter({
			// Serve index.html as the SPA fallback for unknown paths
			fallback: 'index.html'
		})
	}
};

export default config;
