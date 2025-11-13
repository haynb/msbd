import { z } from "zod";

export interface GuardianClientOptions {
  baseUrl: string;
  token: string;
  fetchImpl?: typeof fetch;
}

export const guardianEventSchema = z.object({
  detector: z.string(),
  kind: z.string(),
  processName: z.string().optional(),
  details: z.record(z.any()).optional(),
  actions: z.array(z.string()).default([]),
  occurredAt: z.string().default(() => new Date().toISOString())
});

export type GuardianEventPayload = z.infer<typeof guardianEventSchema>;

export interface GuardianEventResult {
  eventId: string;
}

export async function postGuardianEvent(
  options: GuardianClientOptions & { payload: GuardianEventPayload }
): Promise<GuardianEventResult> {
  const { baseUrl, token, payload, fetchImpl = fetch } = options;
  const body = guardianEventSchema.parse(payload);
  const response = await fetchImpl(new URL("/api/guardian/events", baseUrl).toString(), {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json"
    },
    body: JSON.stringify(body)
  });

  if (!response.ok) {
    throw new Error(`guardian event rejected: ${response.status}`);
  }

  const result = await response.json();
  return result;
}

const screenshotUploadSchema = z.object({
  artifactId: z.string(),
  uploadUrl: z.string().url(),
  headers: z.record(z.string()).default({})
});

export type ScreenshotUploadTicket = z.infer<typeof screenshotUploadSchema>;

export async function requestScreenshotUpload(
  options: GuardianClientOptions
): Promise<ScreenshotUploadTicket> {
  const { baseUrl, token, fetchImpl = fetch } = options;
  const response = await fetchImpl(new URL("/api/guardian/screenshots/request", baseUrl).toString(), {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`
    }
  });

  if (!response.ok) {
    throw new Error(`screenshot ticket failure: ${response.status}`);
  }

  const payload = await response.json();
  return screenshotUploadSchema.parse(payload);
}

export interface ScreenshotUploadConfirmation {
  artifactId: string;
  keyId: string;
  iv: string;
  mac: string;
  bytes: number;
  trigger: string;
}

export async function confirmScreenshotUpload(
  options: GuardianClientOptions & { confirmation: ScreenshotUploadConfirmation }
): Promise<void> {
  const { baseUrl, token, confirmation, fetchImpl = fetch } = options;
  const response = await fetchImpl(new URL("/api/guardian/screenshots/confirm", baseUrl).toString(), {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json"
    },
    body: JSON.stringify(confirmation)
  });

  if (!response.ok) {
    throw new Error(`screenshot confirm failure: ${response.status}`);
  }
}
