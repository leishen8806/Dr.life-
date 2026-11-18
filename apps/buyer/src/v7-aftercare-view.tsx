import { useState, type ChangeEvent } from "react";
import { Button, Card, FormField, StatusTag } from "@drlife/ui";
import type { TranslationKey } from "@drlife/i18n";

type Translate = (
  key: TranslationKey,
  params?: Record<string, string | number>
) => string;
export type ReceiptMethod = "photo" | "signature" | "code";

export function AftercareView({
  t,
  onBack,
  delivered = true
}: {
  t: Translate;
  onBack: () => void;
  delivered?: boolean;
}) {
  const [receipt, setReceipt] = useState<ReceiptMethod>();
  const [confirmed, setConfirmed] = useState(false);
  const [problemOpen, setProblemOpen] = useState(false);
  const [problem, setProblem] = useState({
    type: "damage",
    description: "",
    files: [] as File[]
  });
  const [filePreviews, setFilePreviews] = useState<string[]>([]);
  const [sent, setSent] = useState(false);
  const canConfirm = delivered && !!receipt && !confirmed;
  const onFiles = (event: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.target.files ?? []).slice(0, 6);
    setProblem({ ...problem, files });
    setFilePreviews(
      files
        .filter((file) => file.type.startsWith("image/"))
        .map((file) => URL.createObjectURL(file))
    );
  };
  return (
    <main className="buyer-page">
      <section className="buyer-page__body">
        <Button testId="buyer-back" variant="text" onClick={onBack}>
          {t("common.back")}
        </Button>
        <Card>
          <h1 className="buyer-section__title">{t("buyer.orderAftercare")}</h1>
          <StatusTag tone={confirmed ? "success" : "warning"}>
            {confirmed ? t("buyer.signed") : t("buyer.delivered")}
          </StatusTag>
          {delivered && !confirmed ? (
            <>
              <h2>{t("buyer.receiptProof")}</h2>
              <div className="v7-choice-grid">
                {(["photo", "signature", "code"] as const).map((value) => (
                  <button
                    type="button"
                    key={value}
                    data-testid={`buyer-receipt-${value}`}
                    aria-pressed={receipt === value}
                    onClick={() => setReceipt(value)}
                  >
                    {t(`buyer.receipt.${value}` as TranslationKey)}
                  </button>
                ))}
              </div>
              <Button
                testId="buyer-confirm-receipt"
                disabled={!canConfirm}
                onClick={() => {
                  if (import.meta.env.DEV) setConfirmed(true);
                }}
              >
                {t("buyer.confirmReceipt")}
              </Button>
            </>
          ) : null}
          <Button
            variant="secondary"
            testId="buyer-report-problem"
            onClick={() => setProblemOpen(!problemOpen)}
          >
            {t("buyer.reportProblem")}
          </Button>
          {!import.meta.env.DEV && problemOpen && (
            <p role="status">{t("common.comingSoon")}</p>
          )}
          {problemOpen && import.meta.env.DEV && !sent ? (
            <form
              onSubmit={(event) => {
                event.preventDefault();
                if (problem.description.trim()) setSent(true);
              }}
            >
              <FormField label={t("buyer.problemType")}>
                <select
                  value={problem.type}
                  onChange={(event) =>
                    setProblem({ ...problem, type: event.target.value })
                  }
                >
                  <option value="damage">{t("buyer.problem.damage")}</option>
                  <option value="missing">{t("buyer.problem.missing")}</option>
                  <option value="other">{t("buyer.problem.other")}</option>
                </select>
              </FormField>
              <FormField label={t("buyer.description")}>
                <textarea
                  required
                  value={problem.description}
                  onChange={(event) =>
                    setProblem({ ...problem, description: event.target.value })
                  }
                />
              </FormField>
              <FormField label={t("buyer.photos")}>
                <label className="buyer-upload-button">
                  <span>{t("buyer.choosePhotos")}</span>
                  <input
                    className="buyer-upload-input"
                    type="file"
                    accept="image/*"
                    multiple
                    onChange={onFiles}
                  />
                </label>
                <small>{problem.files.length}/6</small>
                {filePreviews.length > 0 && (
                  <div className="buyer-file-previews">
                    {filePreviews.map((src) => (
                      <img key={src} src={src} alt={t("buyer.photos")} />
                    ))}
                  </div>
                )}
              </FormField>
              <Button type="submit">{t("common.submit")}</Button>
            </form>
          ) : null}
          {sent && <p role="status">{t("buyer.problemSent")}</p>}
        </Card>
      </section>
    </main>
  );
}
