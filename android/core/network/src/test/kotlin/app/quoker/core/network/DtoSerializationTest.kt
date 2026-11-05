package app.quoker.core.network

import app.quoker.core.network.di.QuokerJson
import app.quoker.core.network.model.AdminStatusDto
import app.quoker.core.network.model.AdminUsersResponseDto
import app.quoker.core.network.model.AppVersionDto
import app.quoker.core.network.model.CreateSendRequestDto
import app.quoker.core.network.model.CreatedInviteDto
import app.quoker.core.network.model.ErrorBodyDto
import app.quoker.core.network.model.HistoryPageDto
import app.quoker.core.network.model.InvitesResponseDto
import app.quoker.core.network.model.MeDto
import app.quoker.core.network.model.PrepareResponseDto
import app.quoker.core.network.model.RegisterResponseDto
import app.quoker.core.network.model.ResolveResponseDto
import app.quoker.core.network.model.SendResponseDto
import app.quoker.core.network.model.TargetResponseDto
import app.quoker.core.network.model.TargetsResponseDto
import app.quoker.core.network.model.TaskDto
import app.quoker.core.network.model.TelegramLinkDto
import app.quoker.core.network.model.TelegramStatusDto
import app.quoker.core.network.model.VideoDto
import app.quoker.core.network.model.WorkersResponseDto
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DtoSerializationTest {
    private val json = QuokerJson

    @Test
    fun video() {
        val v = json.decodeFromString<VideoDto>(Samples.VIDEO)
        assertEquals("tiktok:7301234567890123456", v.id)
        assertEquals(27L, v.durationS)
        assertEquals(1080, v.width)
        assertEquals(true, v.noWatermark)
        assertEquals("/v1/videos/tiktok:7301234567890123456/thumbnail", v.thumbnailUrl)
        assertEquals("ready", v.file.status)
        assertEquals(14680064L, v.file.sizeBytes)

        val bare = json.decodeFromString<VideoDto>(Samples.VIDEO_BARE)
        assertNull(bare.author)
        assertNull(bare.durationS)
        assertNull(bare.noWatermark)
        assertNull(bare.thumbnailUrl)
        assertEquals("none", bare.file.status)
        assertNull(bare.file.sizeBytes)
    }

    @Test
    fun auth() {
        val me = json.decodeFromString<MeDto>(Samples.ME)
        assertEquals(MeDto(7, "admin", me.telegram), me)
        assertTrue(me.telegram.linked)
        assertEquals("max", me.telegram.username)
        assertEquals("Max", me.telegram.firstName)

        val reg = json.decodeFromString<RegisterResponseDto>(Samples.REGISTER)
        assertEquals("s3cr3t-token", reg.sessionToken)
        assertFalse(reg.user.telegram.linked)
        assertNull(reg.user.telegram.username)

        val link = json.decodeFromString<TelegramLinkDto>(Samples.TELEGRAM_LINK)
        assertEquals("2026-11-03T12:10:00Z", link.expiresAt)

        // username/first_name are omitted (not null) by the core while pending.
        val pending = json.decodeFromString<TelegramStatusDto>(Samples.TELEGRAM_STATUS_PENDING)
        assertEquals(TelegramStatusDto("pending", null, null), pending)
        val linked = json.decodeFromString<TelegramStatusDto>(Samples.TELEGRAM_STATUS_LINKED)
        assertEquals(TelegramStatusDto("linked", "max", "Max"), linked)
    }

    @Test
    fun resolveAndTasks() {
        val ready = json.decodeFromString<ResolveResponseDto>(Samples.RESOLVE_200)
        assertEquals("tiktok:7301234567890123456", ready.video?.id)
        assertNull(ready.taskId)
        val accepted = json.decodeFromString<ResolveResponseDto>(Samples.TASK_ID)
        assertNull(accepted.video)
        assertEquals("t_01JABC", accepted.taskId)

        val running = json.decodeFromString<TaskDto>(Samples.TASK_RUNNING)
        assertEquals(40, running.progress)
        assertNull(running.video)
        assertNull(running.error)
        assertNull(json.decodeFromString<TaskDto>(Samples.TASK_QUEUED).progress)
        assertEquals("done", json.decodeFromString<TaskDto>(Samples.TASK_DONE).status)
        val failed = json.decodeFromString<TaskDto>(Samples.TASK_FAILED)
        assertEquals("private_video", failed.error?.code)
        assertEquals(false, failed.error?.retryable)

        val prepared = json.decodeFromString<PrepareResponseDto>(Samples.PREPARE_200)
        assertEquals(14680064L, prepared.file?.sizeBytes)
        assertEquals("t_01JABC", json.decodeFromString<PrepareResponseDto>(Samples.TASK_ID).taskId)
    }

    @Test
    fun targets() {
        val list = json.decodeFromString<TargetsResponseDto>(Samples.TARGETS).items
        assertEquals(2, list.size)
        assertNull(list[0].inactiveReason)
        assertEquals("2026-11-02T12:40:00Z", list[0].lastUsedAt)
        assertEquals("bot_not_admin", list[1].inactiveReason)
        assertTrue(list[1].hidden)
        assertNull(list[1].lastUsedAt)
        assertEquals(43L, json.decodeFromString<TargetResponseDto>(Samples.TARGET_PATCHED).target.id)
    }

    @Test
    fun sends() {
        val queued = json.decodeFromString<SendResponseDto>(Samples.SEND_QUEUED).send
        assertEquals(1001L, queued.id)
        assertNull(queued.error)
        val failed = json.decodeFromString<SendResponseDto>(Samples.SEND_FAILED).send
        assertEquals("target_inactive", failed.error?.code)
    }

    @Test
    fun sendRequestAlwaysCarriesCaptionAndSignFlag() {
        val body = json.encodeToString(CreateSendRequestDto.serializer(), CreateSendRequestDto("v", 1, null, false))
        assertEquals("""{"video_id":"v","target_id":1,"caption":null,"sign_as_user":false}""", body)
    }

    @Test
    fun history() {
        val page = json.decodeFromString<HistoryPageDto>(Samples.HISTORY)
        assertEquals("c_4", page.nextCursor)
        assertEquals(3, page.items[0].actions.size)
        assertEquals(1001L, page.items[0].actions[0].sendId)
        assertNull(page.items[0].actions[1].targetTitle)
        assertEquals("send_failed", page.items[1].actions[0].type)
        val last = json.decodeFromString<HistoryPageDto>(Samples.HISTORY_LAST)
        assertTrue(last.items.isEmpty())
        assertNull(last.nextCursor)
    }

    @Test
    fun appVersion() {
        val v = json.decodeFromString<AppVersionDto>(Samples.APP_VERSION)
        assertEquals(AppVersionDto(12, "1.2.0", "https://api.example.ru/download/quoker-1.2.0.apk", "ab12", 10, "Новое"), v)
    }

    @Test
    fun admin() {
        val status = json.decodeFromString<AdminStatusDto>(Samples.ADMIN_STATUS)
        assertEquals(2, status.workers.size)
        assertNull(status.workers[1].version)
        assertEquals("2027-11-12T00:00:00Z", status.workers[0].certNotAfter)
        assertNull(status.workers[1].certNotAfter) // Older cores omit the field.
        assertEquals(3, status.queue.queued)
        assertEquals(1, status.errors24h["tiktok"])
        assertEquals(4000L, status.disk.totalBytes)
        assertEquals("", status.versions["app_latest"])

        assertTrue(json.decodeFromString<WorkersResponseDto>(Samples.WORKERS).items.single().isPoller)
        val invites = json.decodeFromString<InvitesResponseDto>(Samples.INVITES).items
        assertNull(invites[0].usedBy)
        assertEquals(8L, invites[1].usedBy)
        assertEquals("K7Q2-ZX9P", json.decodeFromString<CreatedInviteDto>(Samples.INVITE_CREATED).code)
        val users = json.decodeFromString<AdminUsersResponseDto>(Samples.USERS).items
        assertEquals("blocked", users[1].status)
        assertNull(users[1].lastSeenAt)
    }

    @Test
    fun error() {
        val e = json.decodeFromString<ErrorBodyDto>(Samples.error("rate_limited", "Слишком много", true)).error
        assertEquals("rate_limited", e.code)
        assertTrue(e.retryable)
    }

    @Test
    fun unknownKeysAndNullsForDefaultedFieldsAreTolerated() {
        val me = json.decodeFromString<MeDto>("""{"id":1,"role":"user","telegram":null,"extra":1}""")
        assertFalse(me.telegram.linked)
    }
}
