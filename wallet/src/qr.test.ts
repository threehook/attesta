import jpeg from 'jpeg-js'
import { PNG } from 'pngjs'
import QRCode from 'qrcode'
import { describe, expect, it } from 'vitest'
import { readQrImage } from './qr.js'

const link = 'openid-credential-offer://?credential_offer_uri=https%3A%2F%2Fissuer.example%2Foid4vci%2Fdiploma%2Foffers%2F0b9f0c0e-1111-4222-8333-444455556666'

describe('readQrImage', () => {
  it('reads the text of a QR code in a PNG', async () => {
    expect(readQrImage(await QRCode.toBuffer(link, { type: 'png', width: 480, margin: 2 }))).toBe(link)
  })

  it('reads a QR code that was saved as a JPEG', async () => {
    const png = PNG.sync.read(await QRCode.toBuffer(link, { type: 'png', width: 480, margin: 2 }))
    const jpg = jpeg.encode({ data: png.data, width: png.width, height: png.height }, 90).data

    expect(readQrImage(jpg)).toBe(link)
  })

  it('says so when the image has no QR code', () => {
    const blank = new PNG({ width: 50, height: 50 })
    blank.data.fill(255)

    expect(() => readQrImage(PNG.sync.write(blank))).toThrow('geen QR-code')
  })

  it('says so when the file is not a PNG or a JPEG', () => {
    expect(() => readQrImage(Buffer.from('not an image'))).toThrow('geen PNG- of JPEG')
  })

  it('says so when an image is broken', () => {
    expect(() => readQrImage(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3]))).toThrow('kan niet worden gelezen')
  })
})
