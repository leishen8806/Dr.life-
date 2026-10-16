import { Children, isValidElement, type ReactNode } from "react";
import { readFileSync } from "node:fs";
import { describe, expect, it, vi } from "vitest";
import {
  AppHeader,
  BottomNavigation,
  BottomSheet,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  ListItem,
  Loading,
  StatusTag,
  Stepper
} from "./index";

function findByRole(node: ReactNode, role: string): { props: Record<string, unknown> } | undefined {
  if (!isValidElement<{ role?: string; children?: ReactNode }>(node)) return undefined;
  if (node.props.role === role) return node as { props: Record<string, unknown> };
  return Children.toArray(node.props.children)
    .map((child) => findByRole(child, role))
    .find(Boolean);
}

function findButton(node: ReactNode, label: string): { props: Record<string, unknown> } | undefined {
  if (!isValidElement<{ children?: ReactNode }>(node)) return undefined;
  if (node.type === "button" && node.props.children === label) return node as { props: Record<string, unknown> };
  return Children.toArray(node.props.children)
    .map((child) => findButton(child, label))
    .find(Boolean);
}

function expectWrappingRule(styles: string, selector: string) {
  let start = styles.indexOf(selector);
  while (start >= 0) {
    const end = styles.indexOf("}", start);
    if (styles.slice(start, end).includes("overflow-wrap: anywhere")) return;
    start = styles.indexOf(selector, start + selector.length);
  }
  expect.fail(`${selector} must allow long text to wrap`);
}

describe("foundation components", () => {
  it("renders the reusable shell with accessible navigation and snapshots", () => {
    const navigation = BottomNavigation({
      ariaLabel: "Primary navigation",
      items: [{ id: "home", label: "Home", icon: "⌂" }, { id: "me", label: "Me", icon: "●", badge: 2 }],
      activeId: "home",
      onChange: vi.fn()
    });
    const view = [
      AppHeader({ slogan: "INFINITE POSSIBILITIES", actions: <button aria-label="AI">AI</button> }),
      Card({ children: ListItem({ title: "Order", description: "Ready", trailing: "1" }) }),
      Stepper({ steps: ["Created", "Shipped", "Delivered"], current: 1 }),
      StatusTag({ tone: "success", children: "Ready" }),
      navigation
    ];

    expect(view).toMatchSnapshot();
    expect(navigation.type).toBe("nav");
    expect(navigation.props["aria-label"]).toBe("Primary navigation");
  });

  it("keeps dialog close controls keyboard reachable", () => {
    const close = vi.fn();
    const retry = vi.fn();
    const sheet = BottomSheet({ open: true, title: "Options", closeLabel: "Close", onClose: close, children: "Content" });
    const confirm = ConfirmDialog({ open: true, title: "Delete", description: "This cannot be undone.", cancelLabel: "Cancel", confirmLabel: "Confirm", onCancel: close, onConfirm: retry });
    const empty = EmptyState({ title: "No orders", actionLabel: "Browse", onAction: retry });
    const error = ErrorState({ title: "Network error", description: "Try again.", retryLabel: "Retry", onRetry: retry });

    expect(findByRole(sheet, "dialog")?.props["aria-modal"]).toBe("true");
    expect(findByRole(confirm, "alertdialog")?.props["aria-modal"]).toBe("true");
    expect(findButton(confirm, "Cancel")?.props.autoFocus).toBe(true);
    (findByRole(confirm, "alertdialog")?.props.onKeyDown as (event: { key: string }) => void)({ key: "Escape" });
    expect(close).toHaveBeenCalledOnce();
    expect(findByRole(empty, "status")).toBeDefined();
    expect(findByRole(error, "alert")).toBeDefined();
  });

  it("marks loading and long-text containers for wrapping", () => {
    const loading = Loading({ label: "កំពុងផ្ទុកព័ត៌មានសម្រាប់ការដឹកជញ្ជូនរបស់អ្នក" });
    expect(loading.props.role).toBe("status");
    expect(loading.props.className).toContain("drl-loading");
    const styles = readFileSync(new URL("./styles.css", import.meta.url), "utf8");
    [
      ".drl-loading",
      ".drl-app-header__slogan",
      ".drl-bottom-navigation__label",
      ".drl-list-item__content > strong",
      ".drl-list-item small",
      ".drl-list-item__trailing"
    ].forEach((selector) => expectWrappingRule(styles, selector));
  });
});
