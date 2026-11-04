package app.quoker.core.common.format

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

class FormatsTest {
    @Test
    fun byteSize() {
        val cases = listOf(
            0L to ByteSize("0", SizeUnit.Bytes),
            1000L to ByteSize("1000", SizeUnit.Bytes),
            2048L to ByteSize("2", SizeUnit.Kilobytes),
            14_680_064L to ByteSize("14", SizeUnit.Megabytes),
            1_572_864L to ByteSize("1,5", SizeUnit.Megabytes),
            1_288_490_189L to ByteSize("1,2", SizeUnit.Gigabytes),
            3_221_225_472L to ByteSize("3", SizeUnit.Gigabytes),
        )
        for ((bytes, want) in cases) assertEquals("$bytes", want, ByteSize.of(bytes))
    }

    @Test
    fun duration() {
        val cases = listOf(0L to "0:00", 27L to "0:27", 65L to "1:05", 725L to "12:05", 3723L to "1:02:03", -5L to "0:00")
        for ((s, want) in cases) assertEquals(want, formatDuration(s))
    }

    @Test
    fun dayBucket() {
        val zone = ZoneId.of("Europe/Moscow")
        val now = Instant.parse("2026-11-04T10:00:00Z") // 13:00 MSK
        val cases = listOf(
            Instant.parse("2026-11-03T21:30:00Z") to DayBucket.Today, // 00:30 MSK on the 4th
            Instant.parse("2026-11-03T20:59:00Z") to DayBucket.Yesterday,
            Instant.parse("2026-11-01T12:00:00Z") to DayBucket.Date(LocalDate.of(2026, 11, 1), sameYear = true),
            Instant.parse("2025-12-31T12:00:00Z") to DayBucket.Date(LocalDate.of(2025, 12, 31), sameYear = false),
        )
        for ((at, want) in cases) assertEquals("$at", want, DayBucket.of(at, now, zone))
    }

    @Test
    fun inviteCode() {
        val cases = listOf(
            "" to "",
            "k7q" to "K7Q",
            "k7q2" to "K7Q2",
            "k7q2m" to "K7Q2-M",
            "K7Q2-M4XP" to "K7Q2-M4XP",
            " k7 q2–m4.xp " to "K7Q2-M4XP",
            "K7Q2M4XPZZ" to "K7Q2-M4XP",
            "абв12" to "12",
        )
        for ((input, want) in cases) assertEquals(input, want, InviteCode.format(input))
        assertTrue(InviteCode.isComplete("k7q2-m4xp"))
        assertFalse(InviteCode.isComplete("K7Q2-M4X"))
        assertEquals("K7Q2M4XP", InviteCode.normalize("K7Q2-M4XP"))
    }
}
