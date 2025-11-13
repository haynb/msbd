use std::{
    fs,
    path::{Path, PathBuf},
    time::{Duration, SystemTime, UNIX_EPOCH},
};

use aes_gcm::{aead::Aead, Aes256Gcm, KeyInit};
use base64::{engine::general_purpose, Engine as _};
use rand::{thread_rng, RngCore};
use serde::{de::DeserializeOwned, Deserialize, Serialize};
use sha2::{Digest, Sha256};
use thiserror::Error;
use zeroize::Zeroize;

const NONCE_SIZE: usize = 12;
const VERSION: u8 = 1;

#[derive(Debug, Error)]
pub enum CacheError {
    #[error("io error: {0}")]
    Io(#[from] std::io::Error),
    #[error("serialization error: {0}")]
    Serde(#[from] serde_json::Error),
    #[error("encryption error")]
    Crypto,
}

#[derive(Serialize, Deserialize)]
struct CacheEnvelope {
    version: u8,
    expires_at: Option<u64>,
    iv: String,
    payload: String,
}

pub struct SecureCache {
    path: PathBuf,
    key: [u8; 32],
}

impl SecureCache {
    pub fn new(path: impl Into<PathBuf>, key_material: &[u8]) -> Self {
        let mut hasher = Sha256::new();
        hasher.update(key_material);
        let digest = hasher.finalize();
        let mut key = [0u8; 32];
        key.copy_from_slice(&digest);
        SecureCache {
            path: path.into(),
            key,
        }
    }

    pub fn store<T: Serialize>(&self, value: &T, ttl: Duration) -> Result<(), CacheError> {
        let cipher = Aes256Gcm::new_from_slice(&self.key).map_err(|_| CacheError::Crypto)?;
        let mut plaintext = serde_json::to_vec(value)?;
        let mut nonce = [0u8; NONCE_SIZE];
        thread_rng().fill_bytes(&mut nonce);
        let ciphertext = cipher
            .encrypt(&nonce.into(), plaintext.as_ref())
            .map_err(|_| CacheError::Crypto)?;
        plaintext.zeroize();
        let expires_at = SystemTime::now()
            .checked_add(ttl)
            .and_then(|instant| instant.duration_since(UNIX_EPOCH).ok())
            .map(|duration| duration.as_secs());

        let envelope = CacheEnvelope {
            version: VERSION,
            expires_at,
            iv: general_purpose::STANDARD.encode(nonce),
            payload: general_purpose::STANDARD.encode(ciphertext),
        };

        let serialized = serde_json::to_string(&envelope)?;
        if let Some(parent) = self.path.parent() {
            fs::create_dir_all(parent)?;
        }
        fs::write(&self.path, serialized)?;
        Ok(())
    }

    pub fn load<T: DeserializeOwned>(&self) -> Result<Option<T>, CacheError> {
        let contents = match fs::read_to_string(&self.path) {
            Ok(data) => data,
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
            Err(error) => return Err(CacheError::Io(error)),
        };
        let mut envelope: CacheEnvelope = serde_json::from_str(&contents)?;
        if let Some(expiry) = envelope.expires_at {
            let expires_at = UNIX_EPOCH + Duration::from_secs(expiry);
            if SystemTime::now() > expires_at {
                let _ = fs::remove_file(&self.path);
                return Ok(None);
            }
        }
        let nonce_bytes = general_purpose::STANDARD
            .decode(envelope.iv.as_bytes())
            .map_err(|_| CacheError::Crypto)?;
        let nonce: [u8; NONCE_SIZE] = nonce_bytes
            .as_slice()
            .try_into()
            .map_err(|_| CacheError::Crypto)?;
        let ciphertext = general_purpose::STANDARD
            .decode(envelope.payload.as_bytes())
            .map_err(|_| CacheError::Crypto)?;
        let cipher = Aes256Gcm::new_from_slice(&self.key).map_err(|_| CacheError::Crypto)?;
        let plaintext = cipher
            .decrypt(&nonce.into(), ciphertext.as_ref())
            .map_err(|_| CacheError::Crypto)?;
        let value = serde_json::from_slice::<T>(&plaintext)?;
        Ok(Some(value))
    }

    pub fn clear(&self) -> Result<(), CacheError> {
        match fs::remove_file(&self.path) {
            Ok(_) => Ok(()),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(error) => Err(CacheError::Io(error)),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;
    use tempfile::tempdir;

    #[test]
    fn roundtrip_value() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("cache.json");
        let cache = SecureCache::new(&path, b"secret-key");
        cache.store(&serde_json::json!({"foo": "bar"}), Duration::from_secs(60)).unwrap();
        let loaded: Option<serde_json::Value> = cache.load().unwrap();
        assert_eq!(loaded.unwrap()["foo"], "bar");
    }

    #[test]
    fn expired_payload_returns_none() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("cache.json");
        let cache = SecureCache::new(&path, b"secret-key");
        cache.store(&serde_json::json!({"foo": "bar"}), Duration::from_millis(10)).unwrap();
        std::thread::sleep(Duration::from_millis(20));
        let loaded: Option<serde_json::Value> = cache.load().unwrap();
        assert!(loaded.is_none());
    }
}
