package astcontext

import (
	"path/filepath"
	"slices"
	"strings"
)

type snapshotLimits struct {
	bytes                          int64
	entries, files, depth          int
	ignoreBytes                    int64
	ignorePatterns, ignoreMatchOps int
}

func defaultSnapshotLimits() snapshotLimits {
	return snapshotLimits{
		bytes: 64 * 1024 * 1024, entries: 50_000, files: 20_000, depth: 64,
		ignoreBytes: 512 * 1024, ignorePatterns: 4096, ignoreMatchOps: 10_000_000,
	}
}

var snapshotSuffixes = []string{
	".c++", ".cc", ".cjs", ".cpp", ".cppm", ".cs", ".css", ".cxx",
	".ex", ".exs", ".gd", ".gemspec", ".go", ".h", ".h++", ".hh",
	".hpp", ".htm", ".html", ".hxx", ".inl", ".ipp", ".ixx", ".java",
	".js", ".jsx", ".kt", ".kts", ".lua", ".markdown", ".md", ".mdown",
	".mdx", ".mjs", ".php", ".php8", ".phps", ".phtml", ".py", ".pyi",
	".rake", ".rb", ".rs", ".ru", ".sc", ".scala", ".scss", ".sql",
	".swift", ".tpp", ".ts", ".tsx", ".vue", ".wlua", ".yaml", ".yml",
}

var snapshotBasenames = []string{"Gemfile", "Rakefile"}
var snapshotIgnoreNames = []string{".gitignore", ".ignore"}
var snapshotDefaultIgnores = []string{
	".git/", ".svn/", ".hg/", "node_modules/", "__pycache__/", ".venv/",
	"venv/", ".tox/", ".mypy_cache/", ".pytest_cache/", ".ruff_cache/",
	".eggs/", "*.egg-info/", ".gradle/", ".idea/", ".vs/", ".vscode/",
	".cursor/", ".zed/", ".fleet/", "__snapshots__/", ".husky/", ".next/",
	".nuxt/", ".svelte-kit/", ".turbo/", ".parcel-cache/", ".vite/",
	".terraform/", "*.min.js", "*.min.mjs", "*.min.cjs", "*.min.css",
	"*.min.html", "*.map",
}

func snapshotSupportedName(name string) bool {
	// Pathlib treats dot basenames and a trailing dot as having no suffix.
	extension := filepath.Ext(name)
	if extension == name || extension == "." {
		extension = ""
	}
	return slices.Contains(snapshotBasenames, name) || slices.Contains(snapshotSuffixes, strings.ToLower(extension))
}

func snapshotVCS(name string) bool { return name == ".git" || name == ".hg" || name == ".svn" }
