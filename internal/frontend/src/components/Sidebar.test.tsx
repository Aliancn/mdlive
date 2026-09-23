import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Sidebar } from "./Sidebar";
import { moveFile } from "../hooks/useApi";
import type { Group, SearchResult } from "../hooks/useApi";
import { ToastProvider } from "./Toast";

vi.mock("../hooks/useApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../hooks/useApi")>();
  return { ...actual, moveFile: vi.fn() };
});

const groups: Group[] = [
  {
    name: "default",
    files: [
      { id: "aaa11111", name: "README.md", path: "/README.md", title: "Getting Started" },
      { id: "bbb22222", name: "GUIDE.md", path: "/GUIDE.md" },
    ],
  },
  {
    name: "docs",
    files: [{ id: "ccc33333", name: "api.md", path: "/docs/api.md" }],
  },
];

const searchResults: SearchResult[] = [
  {
    fileId: "aaa11111",
    fileName: "README.md",
    title: "Getting Started",
    path: "/README.md",
    uploaded: false,
    matches: [
      {
        line: 3,
        text: "cache line",
        before: ["# Intro"],
        after: ["after line"],
        heading: "Intro",
        anchor: { kind: "heading", value: "Intro" },
      },
    ],
  },
];

function hasTextContent(text: string) {
  return (_content: string, element: Element | null) => element?.textContent === text;
}

beforeEach(() => {
  localStorage.clear();
});

describe("Sidebar", () => {
  it("renders files for the active group", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.getByText("README.md")).toBeInTheDocument();
    expect(screen.getByText("GUIDE.md")).toBeInTheDocument();
    expect(screen.queryByText("api.md")).not.toBeInTheDocument();
  });

  it("renders files for a non-default group", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="docs"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.getByText("api.md")).toBeInTheDocument();
    expect(screen.queryByText("README.md")).not.toBeInTheDocument();
  });

  it("highlights the active file", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={"aaa11111"}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    const activeLink = screen.getByText("README.md").closest("a")!;
    expect(activeLink.className).toContain("bg-gh-bg-active");
    expect(activeLink.getAttribute("aria-current")).toBe("page");

    const inactiveLink = screen.getByText("GUIDE.md").closest("a")!;
    expect(inactiveLink.className).toContain("bg-transparent");
    expect(inactiveLink.getAttribute("aria-current")).toBeNull();
  });

  it("calls onFileSelect when a file is clicked", async () => {
    const user = userEvent.setup();
    const onFileSelect = vi.fn();
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={onFileSelect}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );

    await user.click(screen.getByText("GUIDE.md"));
    expect(onFileSelect).toHaveBeenCalledWith("bbb22222");
  });

  it("renders file items as anchors with href to file URL", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.getByText("README.md").closest("a")?.getAttribute("href")).toBe(
      "/?file=aaa11111",
    );
    expect(screen.getByText("GUIDE.md").closest("a")?.getAttribute("href")).toBe("/?file=bbb22222");
  });

  it("does not call onFileSelect when modifier keys are pressed", async () => {
    const user = userEvent.setup();
    const onFileSelect = vi.fn();
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={onFileSelect}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );

    await user.keyboard("[ControlLeft>]");
    await user.click(screen.getByText("GUIDE.md"));
    await user.keyboard("[/ControlLeft]");
    expect(onFileSelect).not.toHaveBeenCalled();
  });

  it("shows file path as title attribute", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.getByTitle("/README.md")).toBeInTheDocument();
    expect(screen.getByTitle("/GUIDE.md")).toBeInTheDocument();
  });

  it("renders empty when group has no files", () => {
    const emptyGroups: Group[] = [{ name: "empty", files: [] }];
    render(
      <Sidebar
        groups={emptyGroups}
        activeGroup="empty"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("shows search input when searchQuery is non-null", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery=""
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.getByPlaceholderText("Search files...")).toBeInTheDocument();
  });

  it("does not show search input when searchQuery is null", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.queryByPlaceholderText("Search files...")).not.toBeInTheDocument();
  });

  it("filters files by search query", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery="read"
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.getByText("README.md")).toBeInTheDocument();
    expect(screen.queryByText("GUIDE.md")).not.toBeInTheDocument();
  });

  it("calls onSearchQueryChange with null on Escape key", async () => {
    const user = userEvent.setup();
    const onSearchQueryChange = vi.fn();
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery=""
        onSearchQueryChange={onSearchQueryChange}
      />,
    );
    const input = screen.getByPlaceholderText("Search files...");
    await user.click(input);
    await user.keyboard("{Escape}");
    expect(onSearchQueryChange).toHaveBeenCalledWith(null);
  });

  it("shows heading titles when showTitle is true", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={true}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.getByText("Getting Started")).toBeInTheDocument();
    expect(screen.queryByText("README.md")).not.toBeInTheDocument();
    expect(screen.getByText("GUIDE.md")).toBeInTheDocument();
  });

  it("shows file names when showTitle is false even if title exists", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.getByText("README.md")).toBeInTheDocument();
    expect(screen.queryByText("Getting Started")).not.toBeInTheDocument();
  });

  it("search matches against title", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery="getting"
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.getByText("README.md")).toBeInTheDocument();
    expect(screen.queryByText("GUIDE.md")).not.toBeInTheDocument();
  });

  it("renders content search results", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery="cache"
        onSearchQueryChange={() => {}}
        searchResults={searchResults}
      />,
    );
    expect(screen.getByText("Content matches")).toBeInTheDocument();
    expect(screen.getByText("Line 3")).toBeInTheDocument();
    expect(screen.getByText(hasTextContent("cache line"))).toBeInTheDocument();
  });

  it("shows a toast instead of an alert when moving a file fails", async () => {
    const user = userEvent.setup();
    const alertSpy = vi.spyOn(window, "alert");
    vi.mocked(moveFile).mockRejectedValueOnce(new Error("group not found"));
    render(
      <ToastProvider>
        <Sidebar
          groups={groups}
          activeGroup="default"
          activeFileId="aaa11111"
          onFileSelect={() => {}}
          onFilesReorder={() => {}}
          viewMode="flat"
          showTitle={false}
          searchQuery={null}
          onSearchQueryChange={() => {}}
        />
      </ToastProvider>,
    );

    await user.click(screen.getAllByTitle("More actions")[0]);
    await user.click(screen.getByRole("button", { name: "docs" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("group not found");
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it("toggles content matches section", async () => {
    const user = userEvent.setup();
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery="cache"
        onSearchQueryChange={() => {}}
        searchResults={searchResults}
      />,
    );

    await user.click(screen.getByRole("button", { name: /content matches/i }));
    expect(screen.queryByText(hasTextContent("cache line"))).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /content matches/i }));
    expect(screen.getByText(hasTextContent("cache line"))).toBeInTheDocument();
  });

  it("scrolls the active file into view when it changes", async () => {
    const original = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = vi.fn();
    const view = (activeFileId: string | null) => (
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId={activeFileId}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />
    );
    const { rerender } = render(view("aaa11111"));
    vi.mocked(Element.prototype.scrollIntoView).mockClear();
    rerender(view("bbb22222"));
    await vi.waitFor(() => {
      expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith({ block: "nearest" });
    });
    Element.prototype.scrollIntoView = original;
  });

  it("keeps the file menu visible on the active row without hover", () => {
    render(
      <Sidebar
        groups={groups}
        activeGroup="default"
        activeFileId="aaa11111"
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    const menus = screen.getAllByTitle("More actions");
    expect(menus).toHaveLength(2);
    // The active row's menu is always visible; other rows show it on hover
    // or keyboard focus.
    expect(menus[0].className).toContain("opacity-100");
    expect(menus[0].className).not.toContain("opacity-0");
    expect(menus[1].className).toContain("opacity-0");
  });

  it("shows the directory for duplicate file names in flat view", () => {
    const dupGroups: Group[] = [
      {
        name: "default",
        files: [
          {
            id: "dup1",
            name: "README.md",
            path: "/a/README.md",
            segments: ["a", "README.md"],
          },
          {
            id: "dup2",
            name: "README.md",
            path: "/b/README.md",
            segments: ["b", "README.md"],
          },
          {
            id: "uniq",
            name: "GUIDE.md",
            path: "/a/GUIDE.md",
            segments: ["a", "GUIDE.md"],
          },
        ],
      },
    ];
    render(
      <Sidebar
        groups={dupGroups}
        activeGroup="default"
        activeFileId={null}
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="flat"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.getAllByText("README.md")).toHaveLength(2);
    // Duplicate rows show their directory as a second line...
    expect(screen.getByText("a")).toBeInTheDocument();
    expect(screen.getByText("b")).toBeInTheDocument();
    // ...unique names do not.
    expect(screen.getByText("GUIDE.md").closest("a")?.textContent).toBe("GUIDE.md");
  });

  it("collapses and expands all directories in tree view", async () => {
    const user = userEvent.setup();
    const nestedGroups: Group[] = [
      {
        name: "default",
        files: [
          {
            id: "nested1",
            name: "deep.md",
            path: "/repo/docs/guide/deep.md",
            segments: ["repo", "docs", "guide", "deep.md"],
          },
          {
            id: "root1",
            name: "README.md",
            path: "/repo/README.md",
            segments: ["repo", "README.md"],
          },
        ],
      },
    ];
    render(
      <Sidebar
        groups={nestedGroups}
        activeGroup="default"
        activeFileId="root1"
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="tree"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    expect(screen.getByText("deep.md")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Collapse all" }));
    expect(screen.queryByText("deep.md")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Expand all" }));
    expect(screen.getByText("deep.md")).toBeInTheDocument();
  });

  it("expands collapsed ancestors of the active file in tree view", () => {
    const nestedGroups: Group[] = [
      {
        name: "default",
        files: [
          {
            id: "nested1",
            name: "deep.md",
            path: "/repo/docs/guide/deep.md",
            segments: ["repo", "docs", "guide", "deep.md"],
          },
          {
            id: "root1",
            name: "README.md",
            path: "/repo/README.md",
            segments: ["repo", "README.md"],
          },
        ],
      },
    ];
    localStorage.setItem("ml-sidebar-tree-collapsed", JSON.stringify({ default: ["docs/guide"] }));
    render(
      <Sidebar
        groups={nestedGroups}
        activeGroup="default"
        activeFileId="nested1"
        onFileSelect={() => {}}
        onFilesReorder={() => {}}
        viewMode="tree"
        showTitle={false}
        searchQuery={null}
        onSearchQueryChange={() => {}}
      />,
    );
    // The saved collapse state is overridden for the active file's ancestors.
    expect(screen.getByText("deep.md")).toBeInTheDocument();
  });

  describe("recently viewed section", () => {
    const manyGroups: Group[] = [
      {
        name: "default",
        files: Array.from({ length: 6 }, (_, i) => ({
          id: `file${i}`,
          name: `file${i}.md`,
          path: `/file${i}.md`,
        })),
      },
    ];

    it("shows recent files except the active one", () => {
      render(
        <Sidebar
          groups={manyGroups}
          activeGroup="default"
          activeFileId="file5"
          onFileSelect={() => {}}
          onFilesReorder={() => {}}
          viewMode="flat"
          showTitle={false}
          recentFileIds={["file5", "file3", "file1"]}
          searchQuery={null}
          onSearchQueryChange={() => {}}
        />,
      );
      // The active file is already highlighted in the list below; the recent
      // section shows the others (their names appear twice: recent + list).
      expect(screen.getByText("Recent")).toBeInTheDocument();
      expect(screen.getAllByText("file3.md")).toHaveLength(2);
      expect(screen.getAllByText("file1.md")).toHaveLength(2);
      expect(screen.getAllByText("file5.md")).toHaveLength(1);
      // Files never viewed recently appear only in the list.
      expect(screen.getAllByText("file0.md")).toHaveLength(1);
    });

    it("hides the section for short file lists", () => {
      render(
        <Sidebar
          groups={groups}
          activeGroup="default"
          activeFileId="aaa11111"
          onFileSelect={() => {}}
          onFilesReorder={() => {}}
          viewMode="flat"
          showTitle={false}
          recentFileIds={["aaa11111", "bbb22222"]}
          searchQuery={null}
          onSearchQueryChange={() => {}}
        />,
      );
      expect(screen.queryByText("Recent")).not.toBeInTheDocument();
    });

    it("hides the section while searching", () => {
      render(
        <Sidebar
          groups={manyGroups}
          activeGroup="default"
          activeFileId="file5"
          onFileSelect={() => {}}
          onFilesReorder={() => {}}
          viewMode="flat"
          showTitle={false}
          recentFileIds={["file3", "file1"]}
          searchQuery="file"
          onSearchQueryChange={() => {}}
        />,
      );
      expect(screen.queryByText("Recent")).not.toBeInTheDocument();
    });

    it("skips ids that no longer exist in the group", () => {
      render(
        <Sidebar
          groups={manyGroups}
          activeGroup="default"
          activeFileId="file5"
          onFileSelect={() => {}}
          onFilesReorder={() => {}}
          viewMode="flat"
          showTitle={false}
          recentFileIds={["gone", "file3"]}
          searchQuery={null}
          onSearchQueryChange={() => {}}
        />,
      );
      expect(screen.getByText("Recent")).toBeInTheDocument();
      expect(screen.getAllByText("file3.md").length).toBe(2);
      expect(screen.queryByText("gone")).not.toBeInTheDocument();
    });

    it("collapses and expands", async () => {
      const user = userEvent.setup();
      render(
        <Sidebar
          groups={manyGroups}
          activeGroup="default"
          activeFileId="file5"
          onFileSelect={() => {}}
          onFilesReorder={() => {}}
          viewMode="flat"
          showTitle={false}
          recentFileIds={["file3", "file1"]}
          searchQuery={null}
          onSearchQueryChange={() => {}}
        />,
      );
      await user.click(screen.getByRole("button", { name: /recent/i }));
      // Collapsed: only one row per file remains (the main list's).
      expect(screen.getAllByText("file3.md")).toHaveLength(1);
      await user.click(screen.getByRole("button", { name: /recent/i }));
      expect(screen.getAllByText("file3.md")).toHaveLength(2);
    });
  });
});
