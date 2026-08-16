import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const config: Config = {
    title: 'Synopsis[MEMEX]',
    tagline: 'Zero-Infrastructure RAG & Knowledge Graph',
    favicon: 'img/favicon.ico',

    future: {
        v4: true,
    },

    markdown: {
        mermaid: true,
    },

    url: 'https://devmix.github.io',
    baseUrl: 'synopsis/docs/website/build/',

    organizationName: 'devmix',
    projectName: 'synopsis',

    onBrokenLinks: 'throw',

    headTags: [
        {
            tagName: 'link',
            attributes: {rel: 'preconnect', href: 'https://fonts.googleapis.com'},
        },
        {
            tagName: 'link',
            attributes: {
                rel: 'preconnect',
                href: 'https://fonts.gstatic.com',
                crossorigin: 'anonymous',
            },
        },
    ],

    stylesheets: [
        'https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500&family=Manrope:wght@400;500;600&family=Space+Grotesk:wght@500;600;700&display=swap',
    ],

    i18n: {
        defaultLocale: 'en',
        locales: ['en', 'ru'],
        localeConfigs: {
            en: {
                label: 'English',
                htmlLang: 'en-US',
            },
            ru: {
                label: 'Русский',
                htmlLang: 'ru-RU',
            },
        },
    },

    presets: [
        [
            'classic',
            {
                docs: {
                    sidebarPath: './sidebars.ts',
                    editUrl: 'https://github.com/devmix/synopsis/tree/main/docs/website/',
                },
                blog: false,
                theme: {
                    customCss: './src/css/custom.css',
                },
            } satisfies Preset.Options,
        ],
    ],

    themes: [
        '@docusaurus/theme-mermaid',
        [
            '@easyops-cn/docusaurus-search-local',
            {
                hashed: true,
                indexBlog: false,
                indexPages: false,
                language: ['en', 'ru'],
                highlightSearchTermsOnTargetPage: true,
                docsRouteBasePath: '/docs',
                searchBarShortcutKeymap: 'mod+k',
            },
        ],
    ],

    themeConfig: {
        image: 'img/docusaurus-social-card.jpg',
        colorMode: {
            defaultMode: 'dark',
            disableSwitch: false,
            respectPrefersColorScheme: true,
        },
        mermaid: {
            theme: {
                light: 'neutral',
                dark: 'dark',
            },
        },
        navbar: {
            title: 'Synopsis[MEMEX]',
            logo: {
                alt: 'Synopsis Logo',
                src: 'img/logo.svg',
            },
            items: [
                {
                    type: 'docSidebar',
                    sidebarId: 'tutorialSidebar',
                    position: 'left',
                    label: 'Docs',
                },
                {
                    type: 'localeDropdown',
                    position: 'right',
                },
                {
                    href: 'https://github.com/devmix/synopsis',
                    label: 'GitHub',
                    position: 'right',
                },
            ],
        },
        footer: {
            style: 'dark',
            links: [
                {
                    title: 'Documentation',
                    items: [
                        {
                            label: 'Introduction',
                            to: '/docs/intro',
                        },
                        {
                            label: 'Quickstart',
                            to: '/docs/quickstart',
                        },
                        {
                            label: 'The Pipeline',
                            to: '/docs/concepts/pipeline',
                        },
                        {
                            label: 'MCP Tools',
                            to: '/docs/reference/mcp-tools',
                        },
                    ],
                },
                {
                    title: 'Reference',
                    items: [
                        {
                            label: 'CLI',
                            to: '/docs/reference/cli',
                        },
                        {
                            label: 'Config Schema',
                            to: '/docs/reference/config-schema',
                        },
                        {
                            label: 'Database Schema',
                            to: '/docs/reference/database-schema',
                        },
                        {
                            label: 'Roadmap',
                            to: '/docs/roadmap',
                        },
                    ],
                },
                {
                    title: 'Community',
                    items: [
                        {
                            label: 'GitHub',
                            href: 'https://github.com/devmix/synopsis',
                        },
                    ],
                },
            ],
            copyright: `Copyright © ${new Date().getFullYear()} Synopsis[MEMEX] — structured information for AI agents via MCP. Go · SQLite · ONNX · MCP`,
        },
        prism: {
            theme: prismThemes.github,
            darkTheme: prismThemes.dracula,
        },
    } satisfies Preset.ThemeConfig,
};

export default config;
