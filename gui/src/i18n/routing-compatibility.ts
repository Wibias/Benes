import type { Locale, TranslatedLocale } from "./locale-registry.ts";
import { closedText } from "./message.ts";

export type RoutingCompatibilityField = keyof typeof ROUTING_COMPATIBILITY_EN;

const ROUTING_COMPATIBILITY_EN = {
  "maxEvidenceAgeMs": "Maximum evidence age (ms)",
  "unknownEvidence": "Unknown evidence",
  "degradedEvidence": "Degraded evidence",
} as const;

const ROUTING_COMPATIBILITY_OVERRIDES: Partial<Record<TranslatedLocale, Partial<Record<RoutingCompatibilityField, string>>>> = {
  "de": {
    "maxEvidenceAgeMs": "Maximales Evidenzalter (ms)",
    "unknownEvidence": "Unbekannte Evidenz",
    "degradedEvidence": "Eingeschränkte Evidenz",
  },
  "fr": {
    "maxEvidenceAgeMs": "Âge maximal des preuves (ms)",
    "unknownEvidence": "Preuves inconnues",
    "degradedEvidence": "Preuves dégradées",
  },
  "ko": {
    "maxEvidenceAgeMs": "최대 증거 유효 기간 (ms)",
    "unknownEvidence": "알 수 없는 증거",
    "degradedEvidence": "저하된 증거",
  },
  "zh": {
    "maxEvidenceAgeMs": "证据最大有效期（毫秒）",
    "unknownEvidence": "未知证据",
    "degradedEvidence": "降级证据",
  },
  "zh-TW": {
    "maxEvidenceAgeMs": "證據最大有效期限（毫秒）",
    "unknownEvidence": "未知證據",
    "degradedEvidence": "降級證據",
  },
  "ru": {
    "maxEvidenceAgeMs": "Максимальный возраст доказательств (мс)",
    "unknownEvidence": "Неизвестные доказательства",
    "degradedEvidence": "Ухудшенные доказательства",
  },
  "ja": {
    "maxEvidenceAgeMs": "エビデンスの最大有効期間 (ms)",
    "unknownEvidence": "不明なエビデンス",
    "degradedEvidence": "低下したエビデンス",
  },
  "tr": {
    "maxEvidenceAgeMs": "Maksimum kanıt yaşı (ms)",
    "unknownEvidence": "Bilinmeyen kanıt",
    "degradedEvidence": "Bozulmuş kanıt",
  },
};

export function routingCompatibilityFieldLabel(locale: Locale, field: RoutingCompatibilityField): string {
  return closedText(locale, field, ROUTING_COMPATIBILITY_EN, ROUTING_COMPATIBILITY_OVERRIDES);
}

export function routingCompatibilityLabels(locale: Locale): Record<RoutingCompatibilityField, string> {
  return {
    maxEvidenceAgeMs: routingCompatibilityFieldLabel(locale, "maxEvidenceAgeMs"),
    unknownEvidence: routingCompatibilityFieldLabel(locale, "unknownEvidence"),
    degradedEvidence: routingCompatibilityFieldLabel(locale, "degradedEvidence"),
  };
}
