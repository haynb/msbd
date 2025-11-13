mod dxgi;
mod sck;

use anyhow::{anyhow, Result};
use image::{DynamicImage, ImageBuffer, ImageOutputFormat, Rgba};
use rand::Rng;
use std::io::Cursor;

#[derive(Clone, Debug, Default)]
pub struct RedactionRect {
    pub x: u32,
    pub y: u32,
    pub width: u32,
    pub height: u32,
}

#[derive(Clone, Debug, Default)]
pub struct CaptureRequest {
    pub display: Option<u32>,
    pub redactions: Vec<RedactionRect>,
}

#[derive(Clone, Debug)]
pub struct ScreenshotPayload {
    pub bytes: Vec<u8>,
    pub width: u32,
    pub height: u32,
}

pub fn capture_png(request: &CaptureRequest) -> Result<ScreenshotPayload> {
    let mut frame = match capture_frame(request.display) {
        Ok(image) => image,
        Err(err) => {
            tracing::warn!(error = %err, "reverting to fallback screenshot");
            fallback_frame()
        }
    };

    apply_redactions(&mut frame, &request.redactions);
    let capture_width = frame.width();
    let capture_height = frame.height();
    let mut cursor = Cursor::new(Vec::new());
    DynamicImage::ImageRgba8(frame).write_to(&mut cursor, ImageOutputFormat::Png)?;
    let bytes = cursor.into_inner();
    Ok(ScreenshotPayload {
        bytes,
        width: capture_width,
        height: capture_height,
    })
}

#[cfg(target_os = "windows")]
fn capture_frame(display: Option<u32>) -> Result<ImageBuffer<Rgba<u8>, Vec<u8>>> {
    dxgi::capture(display)
}

#[cfg(target_os = "macos")]
fn capture_frame(display: Option<u32>) -> Result<ImageBuffer<Rgba<u8>, Vec<u8>>> {
    sck::capture(display)
}

#[cfg(not(any(target_os = "windows", target_os = "macos")))]
fn capture_frame(_display: Option<u32>) -> Result<ImageBuffer<Rgba<u8>, Vec<u8>>> {
    Err(anyhow!("capture not supported on this platform"))
}

fn fallback_frame() -> ImageBuffer<Rgba<u8>, Vec<u8>> {
    let width = 640;
    let height = 360;
    let mut image = ImageBuffer::new(width, height);
    let mut rng = rand::thread_rng();
    for (x, y, pixel) in image.enumerate_pixels_mut() {
        let shade = (((x + y) % 255) as u8).saturating_add(rng.gen_range(0..5));
        *pixel = Rgba([shade, shade, shade, 255]);
    }
    image
}

fn apply_redactions(image: &mut ImageBuffer<Rgba<u8>, Vec<u8>>, redactions: &[RedactionRect]) {
    for rect in redactions {
        let max_x = (rect.x + rect.width).min(image.width());
        let max_y = (rect.y + rect.height).min(image.height());
        for y in rect.y..max_y {
            for x in rect.x..max_x {
                image.put_pixel(x, y, Rgba([0, 0, 0, 255]));
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn redactions_zero_pixels() {
        let mut frame = fallback_frame();
        apply_redactions(
            &mut frame,
            &[RedactionRect {
                x: 0,
                y: 0,
                width: 10,
                height: 10,
            }],
        );
        assert_eq!(frame.get_pixel(0, 0)[0], 0);
    }
}
