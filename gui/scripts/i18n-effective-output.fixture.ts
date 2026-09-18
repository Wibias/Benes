/**
 * Frozen effective-output catalogue identity.
 *
 * The canonical digest also moved with #252: the unmounted Integrations-era Grok editor was
 * removed, so its grok.* copy and the Claude-Desktop context labels only that editor still
 * read left the catalogue with it. Grok carries no model list of its own, so the Harnesses
 * detail renders one read-only harnesses.registration.* summary instead.
 * The canonical digest moved with #308: the retired self-update copy and the Overview
 * uptime footer were removed from the catalogue because nothing renders them any more.
 *
 * It moved again with #306, which added `prov.event.warn` and `prov.event.error`: the
 * recent-event lists now announce a warning or a failure instead of leaving the severity
 * to colour alone.
 *
 * It moved again with #331, which deleted the unmounted `startup-sections.tsx` Startup page
 * and `models-preset-control.tsx`. Only those two modules still read the deleted files'
 * `startup.*` and `models.preset*` copy, so the liveness gate pruned 38 keys together with the
 * source that owned them. The #257 Harness sidecar freeze below is byte-identical: no
 * `harnesses.sidecar.*` key was added, removed, or retranslated.
 *
 * It moved again with #327, which added the Claude Desktop settings surface to the Harnesses
 * board: the harnesses.claudeDesktop.* copy states the desired, applied, stale and refusal
 * results the runtime contract #255 reports. No harnesses.sidecar.* key was added, removed,
 * or retranslated.
 */
export const EFFECTIVE_OUTPUT_FIXTURE = {
  canonicalKeyCount: 2202,
  canonicalDigest: "c0c81c923fbd56947eb4c5f22b186def203ad67f73612212d7097e8cc03a5f9b",
  sidecarKeyCount: 13,
  sidecarDigest: "1132373c7ebc5fe38414a4f36307496ae0df6317b382bd2711cef0d975651b1e",
  locales: ["en", "de", "fr", "ko", "zh", "zh-TW", "ru", "ja", "tr"],
  localeDigests: {
    en: "80e3ea805a05f542b9b4750140404bd1376ee5d26dcb784928c0c4ffaf2850c4",
    de: "f67d0d4311c03a88b85fb061019bf31f5c17c3ed1a9e853e4f01d817b4b07404",
    fr: "4142a11d268e612c339aef94e77aa8fcdc7a808740fe56cf7e0c68982f2e3261",
    ko: "2b7e6b15104d42ecd0c487e6f174a4ca902350836313ef24dc1bb52ba387b37a",
    zh: "967d05c95efd98636311911dd7dd720a2623e6c2179ed9037b3a87aecf14e326",
    "zh-TW": "00477ad3203c0d2c47b3d29ddb0176a0e411a3f350b4e14e08f7fd86967dc57c",
    ru: "859d46036a7fa91b95a552353d02f374b890961870bdc6a6ce6e11b812b8ae15",
    ja: "83b27fcb0824c4a4aee49aa7fc106e516ed10e39cfa55ac8decb94c9fcdfb2f1",
    tr: "5c80e6c1516f93abf31c60639dd83c2e9578063453aa40d45a67fe32a5aaad51",
  },
  sidecarLocaleDigests: {
    en: "9941970f2cbe657c7c15676a0d0c87ce60e8b0cdd600cd2de2a9450dbfc7d958",
    de: "9941970f2cbe657c7c15676a0d0c87ce60e8b0cdd600cd2de2a9450dbfc7d958",
    fr: "9941970f2cbe657c7c15676a0d0c87ce60e8b0cdd600cd2de2a9450dbfc7d958",
    ko: "9941970f2cbe657c7c15676a0d0c87ce60e8b0cdd600cd2de2a9450dbfc7d958",
    zh: "9941970f2cbe657c7c15676a0d0c87ce60e8b0cdd600cd2de2a9450dbfc7d958",
    "zh-TW": "9941970f2cbe657c7c15676a0d0c87ce60e8b0cdd600cd2de2a9450dbfc7d958",
    ru: "9941970f2cbe657c7c15676a0d0c87ce60e8b0cdd600cd2de2a9450dbfc7d958",
    ja: "9941970f2cbe657c7c15676a0d0c87ce60e8b0cdd600cd2de2a9450dbfc7d958",
    tr: "9941970f2cbe657c7c15676a0d0c87ce60e8b0cdd600cd2de2a9450dbfc7d958",
  },
  labCatalogKeyCount: 47,
  labOverlayDigest: "727d812c9df3e13826cb70306831fc08248d2935d6138f5dc0eda20d492d08a0",
  labSupplementDigest: "08461f95c725a3a47bcb202cf12346cb0a34c6b25d09f06e2ce673538e22db4d",
  logGuardDigest: "fa32bd11a843ace7b2949999e1ab91d68bfd1ca8d4aaf890f6588673c70c1c01",
  routingCompatibilityDigest: "e389510d951e84f1ad3b775a0793cc1195934d1064c74e5fe63d33c0216f9426",
} as const;
