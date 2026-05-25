import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// The docs are published at the custom GitHub Pages domain. SITE can still be
// overridden for preview builds.
const site = process.env.SITE || 'https://mnemo.biodun.dev';
const base = process.env.BASE_PATH || undefined;

export default defineConfig({
  site,
  base,
  integrations: [
    starlight({
      title: 'Mnemo',
      description:
        'Cross-agent memory for AI coding. Switch tools without losing your place.',
      sidebar: [
        {
          label: 'Start',
          items: [
            { slug: 'index', label: 'Overview' },
            { slug: 'start/understanding-mnemo', label: 'Understanding Mnemo' },
            { slug: 'start/install', label: 'Install' },
            { slug: 'start/quick-start', label: 'Quick start' },
          ],
        },
        {
          label: 'Concepts',
          items: [
            { slug: 'concepts/sessions', label: 'Agents and ingestion' },
            { slug: 'concepts/contexts', label: 'Contexts' },
            { slug: 'concepts/tasks', label: 'Tasks and threading' },
            { slug: 'concepts/state-of-play', label: 'State of play' },
            { slug: 'concepts/resume', label: 'Resume and injection' },
            { slug: 'concepts/privacy', label: 'Privacy and safety' },
          ],
        },
        {
          label: 'Use Mnemo',
          items: [
            { slug: 'guides/cli', label: 'CLI workflow' },
            { slug: 'guides/web-ui-api', label: 'Web UI and API' },
            { slug: 'guides/mcp', label: 'MCP server' },
            { slug: 'guides/configuration', label: 'Configuration' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { slug: 'reference/commands', label: 'Commands' },
            { slug: 'reference/api', label: 'HTTP API' },
            { slug: 'reference/storage', label: 'Storage' },
            { slug: 'reference/project-layout', label: 'Project layout' },
            { slug: 'reference/development', label: 'Development' },
            { slug: 'reference/status', label: 'Status' },
          ],
        },
      ],
    }),
  ],
});
