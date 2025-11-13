use anyhow::{anyhow, Result};
use image::{ImageBuffer, Rgba};

#[cfg(target_os = "macos")]
pub fn capture(display: Option<u32>) -> Result<ImageBuffer<Rgba<u8>, Vec<u8>>> {
    use screenshots::Screen;
    let screens = Screen::all()?.into_iter().enumerate().collect::<Vec<_>>();
    let target_index = display.unwrap_or(0) as usize;
    let screen = screens
        .get(target_index)
        .map(|(_, screen)| screen)
        .or_else(|| screens.first().map(|(_, screen)| screen))
        .ok_or_else(|| anyhow!("no displays available"))?;
    let image = screen.capture()?;
    let width = image.width() as usize;
    let height = image.height() as usize;
    let mut buffer = ImageBuffer::new(width as u32, height as u32);
    for y in 0..height {
        for x in 0..width {
            let idx = (y * width + x) * 4;
            let pixel = image.rgba()[idx..idx + 4].try_into().unwrap();
            buffer.put_pixel(x as u32, y as u32, Rgba(pixel));
        }
    }
    Ok(buffer)
}

#[cfg(not(target_os = "macos"))]
pub fn capture(_display: Option<u32>) -> Result<ImageBuffer<Rgba<u8>, Vec<u8>>> {
    Err(anyhow!("screen capture kit unavailable"))
}
