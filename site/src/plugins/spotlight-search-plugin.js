/**
 * Generates a JSON search index from markdown files in the docs directory.
 * Scans all .md/.mdx files and extracts title + content for search.
 */
const fs = require('fs');
const path = require('path');

module.exports = function (context, options) {
  const { siteDir, baseUrl } = context;
  const docsDir = path.join(siteDir, 'docs');
  const searchPath = options.searchPath || docsDir;

  function extractFrontMatter(content) {
    const match = content.match(/^---\n([\s\S]*?)\n---/);
    if (!match) return {};
    const fm = {};
    match[1].split('\n').forEach((line) => {
      const [key, ...rest] = line.split(':');
      if (key && rest.length) {
        fm[key.trim()] = rest.join(':').trim().replace(/^["']|["']$/g, '');
      }
    });
    return fm;
  }

  function stripMarkdown(text) {
    return text
      .replace(/#{1,6}\s*/g, '')
      .replace(/`{1,3}[^`]*`{1,3}/g, '')
      .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
      .replace(/!\[[^\]]*\]\([^)]+\)/g, '')
      .replace(/[*_~`]/g, '')
      .replace(/\n+/g, ' ')
      .trim();
  }

  function findMdFiles(dir) {
    let files = [];
    try {
      const entries = fs.readdirSync(dir, { withFileTypes: true });
      for (const entry of entries) {
        const fullPath = path.join(dir, entry.name);
        if (entry.isDirectory()) {
          if (entry.name !== 'node_modules' && entry.name !== '.docusaurus') {
            files = files.concat(findMdFiles(fullPath));
          }
        } else if (entry.name.match(/\.(md|mdx)$/)) {
          files.push(fullPath);
        }
      }
    } catch {
      // directory doesn't exist
    }
    return files;
  }

  return {
    name: 'spotlight-search-plugin',

    async loadContent() {
      return null;
    },

    async contentLoaded({ content, actions, siteMetadata }) {
      const { createData } = actions;

      // Build index from markdown files
      const searchIndex = [];
      const mdFiles = findMdFiles(searchPath);

      for (const filePath of mdFiles) {
        const content = fs.readFileSync(filePath, 'utf8');
        const fm = extractFrontMatter(content);
        const body = content.replace(/^---\n[\s\S]*?\n---/, '').replace(/^#.*?\n/, '');
        const text = stripMarkdown(body);

        // Generate URL
        const relativePath = path.relative(searchPath, filePath);
        const urlPath = '/' + relativePath.replace(/\.(md|mdx)$/, '').replace(/index$/, '');
        const url = `${baseUrl}docs${urlPath}`;

        searchIndex.push({
          title: fm.title || path.basename(filePath, path.extname(filePath)),
          excerpt: (fm.description || '').length > 100 
            ? (fm.description || '').substring(0, 100) + '...' 
            : (fm.description || text.substring(0, 100)),
          url,
        });
      }

      // Write index to build output
      const json = JSON.stringify(searchIndex, null, 2);
      const buildPath = path.join(siteDir, 'build', 'synopsis', 'spotlight-search-index.json');
      const buildDir = path.dirname(buildPath);
      if (!fs.existsSync(buildDir)) {
        fs.mkdirSync(buildDir, { recursive: true });
      }
      fs.writeFileSync(buildPath, json);
    },
  };
};
