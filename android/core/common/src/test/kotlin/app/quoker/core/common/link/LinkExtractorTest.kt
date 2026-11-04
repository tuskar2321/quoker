package app.quoker.core.common.link

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.io.File

/** Runs the core's shared vectors (copied from internal/shared/urls/testdata/vectors.json). */
class LinkExtractorTest {
    private val vectorsText: String =
        checkNotNull(javaClass.classLoader?.getResource("vectors.json")) { "vectors.json missing" }.readText()

    private val vectors: List<JsonObject> =
        Json.parseToJsonElement(vectorsText).jsonObject.getValue("vectors").jsonArray.map { it.jsonObject }

    private fun JsonObject.str(name: String): String = getValue(name).jsonPrimitive.content

    @Test
    fun copyMatchesCoreVectors() {
        // Unit tests run with the module dir as working dir; the repo root is three levels up.
        val original = File("../../../internal/shared/urls/testdata/vectors.json")
        if (!original.exists()) return
        assertEquals("update core/common/src/test/resources/vectors.json", original.readText(), vectorsText)
    }

    @Test
    fun sharedVectors() {
        assertTrue(vectors.isNotEmpty())
        val names = mutableSetOf<String>()
        for (v in vectors) {
            val name = v.str("name")
            assertTrue("duplicate vector $name", names.add(name))
            val input = v.str("input")
            val result = LinkExtractor.extract(input)
            if ("error" in v) {
                assertEquals(name, LinkResult.Rejected(rejectionOf(v.str("error"))), result)
                assertEquals(name, null, LinkExtractor.firstSupported(input))
                continue
            }
            if (result !is LinkResult.Supported) fail("$name: got $result")
            val link = (result as LinkResult.Supported).link
            assertEquals(name, v.str("platform"), link.platform.id)
            assertEquals(name, v.str("video_id").ifEmpty { null }, link.videoId)
            assertEquals(name, v.getValue("short").jsonPrimitive.boolean, link.isShort)
            assertEquals(name, v.str("key"), link.key)
            assertEquals(name, v.str("url"), link.url)
            assertEquals(name, link, LinkExtractor.firstSupported(input))
            // A normalized URL must normalize to itself.
            assertEquals(name, result, LinkExtractor.normalize(link.url))
        }
    }

    @Test
    fun tooLongIsRejected() {
        val long = "https://www.tiktok.com/@" + "a".repeat(LinkExtractor.MAX_LEN) + "/video/1"
        assertEquals(LinkResult.Rejected(LinkRejection.UnsupportedUrl), LinkExtractor.normalize(long))
    }

    @Test
    fun percentEncodedPathIsDecoded() {
        val result = LinkExtractor.extract("https://www.instagram.com/%72eel/C9xYz_AbC-1/")
        assertEquals("https://instagram.com/reel/C9xYz_AbC-1", (result as LinkResult.Supported).link.key)
    }

    @Test
    fun badEscapeSkipsCandidate() {
        val result = LinkExtractor.extract("https://www.tiktok.com/%zz https://vm.tiktok.com/ZMabc123/")
        assertEquals("https://vm.tiktok.com/ZMabc123", (result as LinkResult.Supported).link.key)
    }

    private fun rejectionOf(code: String) = LinkRejection.entries.first { it.code == code }
}
