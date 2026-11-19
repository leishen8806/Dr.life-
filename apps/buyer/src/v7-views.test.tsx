import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { t as translate } from "@drlife/i18n";
import { DecorView, validateInquiry } from "./v7-views";
import { AftercareView } from "./v7-aftercare-view";

const t = (
  key: Parameters<typeof translate>[0],
  params?: Record<string, string | number>
) => translate(key, params, "en");

describe("buyer V7 pages", () => {
  it("requires every inquiry field before submit", () => {
    expect(
      validateInquiry(
        {
          description: "",
          budget: "",
          duration: "",
          region: "",
          contact: "telegram"
        },
        t
      )
    ).toHaveLength(4);
    expect(
      validateInquiry(
        {
          description: "Kitchen",
          budget: "1000",
          duration: "30",
          region: "Kandal",
          contact: "telegram"
        },
        t
      )
    ).toEqual([]);
  });

  it("renders the find-decoration tabs, six categories, and company cards", () => {
    const markup = renderToStaticMarkup(
      <DecorView
        t={t}
        onBack={() => undefined}
        onOpenNotifications={() => undefined}
      />
    );
    expect(markup).toContain(t("buyer.companies"));
    expect(markup).toContain(t("buyer.cases"));
    expect(
      (markup.match(/aria-pressed="false"/g) ?? []).length
    ).toBeGreaterThanOrEqual(6);
    expect(markup).toContain("Lotus Build");
  });

  it("requires a receipt proof before the delivered order can be confirmed", () => {
    const markup = renderToStaticMarkup(
      <AftercareView t={t} onBack={() => undefined} />
    );
    expect(markup).toContain(t("buyer.receiptProof"));
    expect(markup).toContain(`disabled`);
    expect(markup).toContain(t("buyer.reportProblem"));
  });
});
