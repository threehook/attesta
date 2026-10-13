import jpeg from 'jpeg-js'
import jsqrModule from 'jsqr'
import { PNG } from 'pngjs'

// The package is CommonJS; depending on how it is loaded the function is the module itself or its default export.
type ReadQr = (data: Uint8ClampedArray, width: number, height: number) => { data: string } | null
const jsQR = ((jsqrModule as unknown as { default?: ReadQr }).default ?? jsqrModule) as unknown as ReadQr

/** The text in the QR code of an image (PNG or JPEG), such as the link to a credential offer; throws when the image is not one of those or holds no QR code. */
export function readQrImage(image: Uint8Array): string {
  const bytes = Buffer.from(image)
  const { data, width, height } = decode(bytes)
  const found = jsQR(new Uint8ClampedArray(data), width, height)
  if (!found) throw new Error('Er staat geen QR-code in deze afbeelding.')
  return found.data
}

function decode(bytes: Buffer): { data: Uint8Array; width: number; height: number } {
  try {
    if (bytes.subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))) return PNG.sync.read(bytes)
    if (bytes[0] === 0xff && bytes[1] === 0xd8) return jpeg.decode(bytes, { useTArray: true })
  } catch {
    throw new Error('Deze afbeelding kan niet worden gelezen.')
  }
  throw new Error('Dit is geen PNG- of JPEG-afbeelding.')
}
