import { defineConfig, type DefaultTheme } from 'vitepress';

// https://vitepress.dev/reference/site-config
export default defineConfig({
	title: "PolySchema",
	description: "Schema migrations for multiple database engines",
	head: [
		["link", { rel: "icon", type: "image/svg+xml", href: "/images/favicon/favicon.svg" }],
		["link", { rel: "icon", type: "image/png", sizes: "96x96", href: "/images/favicon/favicon-96x96.png" }],
		["link", { rel: "apple-touch-icon", href: "/images/favicon/apple-touch-icon.png" }],
		["link", { rel: "manifest", href: "/images/favicon/site.webmanifest" }],
		["meta", { name: "description", content: "Schema migrations for multiple database engines" }],
		["meta", { property: "og:title", content: "PolySchema" }],
		["meta", { property: "og:description", content: "Schema migrations for multiple database engines"}],
		["meta", { property: "og:type", content: "website" }],
		["meta", { property: "og:url", content: "https://polyschema.jc21.com/" }],
		["meta", { property: "og:image", content: "https://polyschema.jc21.com/images/favicon/apple-touch-icon.png" }],
		["meta", { name: "twitter:card", content: "summary"}],
		["meta", { name: "twitter:title", content: "PolySchema"}],
		["meta", { name: "twitter:description", content: "Schema migrations for multiple database engines"}],
		["meta", { name: "twitter:image", content: "https://polyschema.jc21.com/images/favicon/apple-touch-icon.png"}],
		["meta", { name: "twitter:alt", content: "PolySchema"}],
		// GA
		// ['script', { async: 'true', src: 'https://www.googletagmanager.com/gtag/js?id=G-TXT8F5WY5B'}],
		// ['script', {}, "window.dataLayer = window.dataLayer || [];\nfunction gtag(){dataLayer.push(arguments);}\ngtag('js', new Date());\ngtag('config', 'G-TXT8F5WY5B');"],
	],
	sitemap: {
		hostname: 'https://polyschema.jc21.com'
	},
	metaChunk: true,
	srcDir: './src',
	outDir: './dist',
	themeConfig: {
		// https://vitepress.dev/reference/default-theme-config
		logo: { src: '/images/favicon/favicon.svg', width: 24, height: 24 },
		nav: [
			{ text: 'Guide', link: '/guide/' },
			{ text: 'Install', link: '/setup/' },
			{ text: 'CLI', link: '/guide/cli' },
			{ text: 'Go Library', link: '/guide/library' },
			{ text: 'Reference', link: '/guide/migrations' },
		],
		sidebar: [
			{
				text: 'Introduction',
				items: [
					{ text: 'What is PolySchema?', link: '/guide/' },
					{ text: 'Installation', link: '/setup/' },
				]
			},
			{
				text: 'Usage',
				items: [
					{ text: 'Command Line', link: '/guide/cli' },
					{ text: 'Go Library', link: '/guide/library' },
				]
			},
			{
				text: 'Reference',
				items: [
					{ text: 'Migration Files', link: '/guide/migrations' },
					{ text: 'Types & Engine Support', link: '/guide/engines' },
					{ text: 'How It Works', link: '/guide/how-it-works' },
				]
			}
		],
		editLink: {
			pattern: 'https://github.com/jc21/polyschema/edit/main/docs/src/:path'
		},
		socialLinks: [
			{ icon: 'github', link: 'https://github.com/jc21/polyschema' }
		],
		search: {
			provider: 'local'
		},
		footer: {
			message: 'Released under the MIT License.',
			copyright: 'Copyright © 2026-present jc21.com'
		}
	}
});
