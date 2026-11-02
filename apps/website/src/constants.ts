export const SITE_URL = "https://settlecore.org";
export const BOT_USERNAMES = {
  buyer: "DrLifePlusBot",
  merchant: "DrLifeMerchantBot"
} as const;

export const EXTERNAL_LINKS = {
  buyerBot: `https://t.me/${BOT_USERNAMES.buyer}`,
  merchantBot: `https://t.me/${BOT_USERNAMES.merchant}`
} as const;
