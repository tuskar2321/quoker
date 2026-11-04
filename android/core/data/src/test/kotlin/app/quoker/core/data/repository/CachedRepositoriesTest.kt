package app.quoker.core.data.repository

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.cash.turbine.test
import app.quoker.core.data.FakeClock
import app.quoker.core.data.TestApi
import app.quoker.core.data.Wire
import app.quoker.core.data.db.QuokerDatabase
import app.quoker.core.data.model.HistoryActionType
import app.quoker.core.data.model.InactiveReason
import app.quoker.core.data.model.TargetStatus
import app.quoker.core.network.di.QuokerJson
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class CachedRepositoriesTest {
    private val t = TestApi()
    private val db = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), QuokerDatabase::class.java)
        .allowMainThreadQueries()
        .build()
    private val targets = TargetsRepositoryImpl(t.api, db.targetDao())
    private val history = HistoryRepositoryImpl(t.api, db.historyDao(), t.urls, QuokerJson, FakeClock(), Dispatchers.IO)

    @After
    fun tearDown() {
        db.close()
        t.close()
    }

    @Test
    fun targetsAreCachedInServerOrder() = runBlocking {
        assertTrue(targets.observe().first().isEmpty())
        t.enqueue(
            200,
            """{"items":[${Wire.target(42, "Друзья")},${Wire.target(7, "Архив", hidden = true)},${Wire.target(9, "Канал", status = "inactive", reason = "bot_not_admin")}]}""",
        )
        assertEquals(3, targets.fetch().size)
        assertEquals("/v1/targets?include_hidden=true", t.server.takeRequest().path)

        val visible = targets.observe().first()
        assertEquals(listOf(42L, 9L), visible.map { it.id })
        assertEquals(TargetStatus.INACTIVE, visible[1].status)
        assertEquals(InactiveReason.BOT_NOT_ADMIN, visible[1].inactiveReason)
        assertEquals(listOf(42L, 7L, 9L), targets.observe(includeHidden = true).first().map { it.id })
    }

    @Test
    fun setHiddenUpdatesCacheInPlace() = runBlocking {
        t.enqueue(200, """{"items":[${Wire.target(42, "Друзья")},${Wire.target(7, "Архив")}]}""")
        targets.fetch()
        targets.observe().test {
            assertEquals(listOf(42L, 7L), awaitItem().map { it.id })
            t.enqueue(200, """{"target":${Wire.target(42, "Друзья", hidden = true)}}""")
            assertTrue(targets.setHidden(42, true).hidden)
            assertEquals(listOf(7L), awaitItem().map { it.id })
        }
        assertEquals(listOf(42L, 7L), targets.observe(includeHidden = true).first().map { it.id })
        t.server.takeRequest()
        val patch = t.server.takeRequest()
        assertEquals("PATCH", patch.method)
        assertEquals("""{"hidden":true}""", patch.body.readUtf8())
    }

    @Test
    fun refreshQueuesRecheck() = runBlocking {
        t.enqueue(202)
        targets.refresh()
        assertEquals("POST /v1/targets/refresh", t.server.takeRequest().let { "${it.method} ${it.path}" })
    }

    @Test
    fun historyFirstPageIsCached() = runBlocking {
        history.cachedFirstPage.test {
            val empty = awaitItem()
            assertTrue(empty.items.isEmpty())
            assertNull(empty.nextCursor)

            t.enqueue(
                200,
                """{"items":[{"id":5,"video":${Wire.VIDEO},"actions":[{"type":"sent","target_title":"Друзья","send_id":1001,"at":"2026-11-03T12:00:05Z"},{"type":"resolved","target_title":null,"send_id":null,"at":"2026-11-03T11:58:00Z"}]},{"id":4,"video":${Wire.VIDEO_BARE},"actions":[{"type":"send_failed","target_title":"Канал","send_id":1000,"at":"2026-11-02T10:00:00Z"}]}],"next_cursor":"c_4"}""",
            )
            val fresh = history.refresh()
            assertEquals("c_4", fresh.nextCursor)

            // Items and meta invalidate separately; wait for the settled page.
            var cached = awaitItem()
            while (cached != fresh) cached = awaitItem()
            cancelAndIgnoreRemainingEvents()
            assertEquals(listOf(5L, 4L), cached.items.map { it.id })
            assertEquals(HistoryActionType.SENT, cached.items[0].actions[0].type)
            assertEquals(1001L, cached.items[0].actions[0].sendId)
            assertEquals(t.urls.thumbnail("tiktok:1"), cached.items[0].video.thumbnailUrl)
            assertEquals(HistoryActionType.SEND_FAILED, cached.items[1].actions[0].type)
        }
        assertEquals("/v1/history?limit=20", t.server.takeRequest().path)
    }

    @Test
    fun followingPagesAreNotCached() = runBlocking {
        t.enqueue(200, """{"items":[{"id":3,"video":${Wire.VIDEO_BARE},"actions":[]}],"next_cursor":null}""")
        val page = history.page("c_4")
        assertNull(page.nextCursor)
        assertEquals("/v1/history?cursor=c_4&limit=20", t.server.takeRequest().path)
        assertTrue(history.cachedFirstPage.first().items.isEmpty())
    }
}
