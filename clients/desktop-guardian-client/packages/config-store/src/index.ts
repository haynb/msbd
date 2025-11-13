import fs from "node:fs/promises";
import path from "node:path";
import crypto from "node:crypto";
import { z } from "zod";
import type { RemoteConfig } from "@guardian/network-client";

export type ConfigSource = "live" | "cache" | "override";

export interface ConfigSnapshot {
  config: RemoteConfig;
  fetchedAt: number;
  expiresAt: number;
  source: ConfigSource;
}

export interface ConfigUpdatePayload {
  config: RemoteConfig;
  source: ConfigSource;
  fetchedAt: number;
  expiresAt: number;
}

interface ConfigEnvelope {
  fetchedAt: number;
  expiresAt: number;
  config: RemoteConfig;
}

const persistedSchema = z.object({
  version: z.literal(1),
  iv: z.string(),
  ciphertext: z.string(),
  tag: z.string(),
  expiresAt: z.number().int().nonnegative()
});

export interface ConfigStoreOptions {
  cacheDir: string;
  filename?: string;
  deviceSecret: Buffer;
  ttlMinutes?: number;
}

export class ConfigStore {
  private readonly filePath: string;
  private readonly key: Buffer;
  private readonly ttlMs: number;

  constructor(options: ConfigStoreOptions) {
    const { cacheDir, filename = "config.snapshot", deviceSecret, ttlMinutes = 30 } = options;
    this.filePath = path.resolve(cacheDir, filename);
    this.key = deriveKey(deviceSecret);
    this.ttlMs = ttlMinutes * 60_000;
  }

  async save(config: RemoteConfig): Promise<ConfigSnapshot> {
    const fetchedAt = Date.now();
    const expiresAt = fetchedAt + this.ttlMs;
    const envelope: ConfigEnvelope = { config, fetchedAt, expiresAt };
    const plaintext = Buffer.from(JSON.stringify(envelope), "utf-8");
    const iv = crypto.randomBytes(12);
    const cipher = crypto.createCipheriv("aes-256-gcm", this.key, iv);
    const ciphertext = Buffer.concat([cipher.update(plaintext), cipher.final()]);
    const tag = cipher.getAuthTag();
    plaintext.fill(0);

    await fs.mkdir(path.dirname(this.filePath), { recursive: true });
    const payload = {
      version: 1,
      iv: iv.toString("base64"),
      ciphertext: ciphertext.toString("base64"),
      tag: tag.toString("base64"),
      expiresAt
    } as const;
    await fs.writeFile(this.filePath, JSON.stringify(payload), { encoding: "utf-8", mode: 0o600 });
    ciphertext.fill(0);
    iv.fill(0);
    tag.fill(0);

    return {
      config,
      fetchedAt,
      expiresAt,
      source: "live"
    } satisfies ConfigSnapshot;
  }

  async load(): Promise<ConfigSnapshot | null> {
    try {
      const raw = await fs.readFile(this.filePath, "utf-8");
      const persisted = persistedSchema.parse(JSON.parse(raw));
      if (Date.now() > persisted.expiresAt) {
        await this.clear();
        return null;
      }
      const iv = Buffer.from(persisted.iv, "base64");
      const ciphertext = Buffer.from(persisted.ciphertext, "base64");
      const tag = Buffer.from(persisted.tag, "base64");
      const decipher = crypto.createDecipheriv("aes-256-gcm", this.key, iv);
      decipher.setAuthTag(tag);
      const decrypted = Buffer.concat([decipher.update(ciphertext), decipher.final()]);
      const envelope = JSON.parse(decrypted.toString("utf-8")) as ConfigEnvelope;
      decrypted.fill(0);
      ciphertext.fill(0);
      iv.fill(0);
      tag.fill(0);
      return {
        config: envelope.config,
        fetchedAt: envelope.fetchedAt,
        expiresAt: envelope.expiresAt,
        source: "cache"
      } satisfies ConfigSnapshot;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ENOENT") {
        return null;
      }
      console.warn("config cache load failed", error);
      return null;
    }
  }

  async clear(): Promise<void> {
    try {
      await fs.rm(this.filePath, { force: true });
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") {
        throw error;
      }
    }
  }
}

export function deriveKey(secret: Buffer | string): Buffer {
  const source = Buffer.isBuffer(secret) ? secret : Buffer.from(secret, "utf-8");
  const hash = crypto.createHash("sha256").update(source).digest();
  source.fill?.(0);
  return hash;
}
