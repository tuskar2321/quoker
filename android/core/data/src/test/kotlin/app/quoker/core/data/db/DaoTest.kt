package app.quoker.core.data.db

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class DaoTest {
    private val db = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), QuokerDatabase::class.java)
        .allowMainThreadQueries()
        .build()

    @After
    fun tearDown() = db.close()

    private fun target(id: Long, position: Int, hidden: Boolean = false) =
        TargetEntity(id, position, "t$id", "group", "active", null, hidden, null)

    @Test
    fun targetsReplaceAndOrder() = runBlocking {
        val dao = db.targetDao()
        dao.replaceAll(listOf(target(1, 1), target(2, 0)))
        assertEquals(listOf(2L, 1L), dao.observeAll().first().map { it.id })
        assertEquals(1, dao.positionOf(1))
        assertNull(dao.positionOf(3))

        dao.replaceAll(listOf(target(3, 0)))
        assertEquals(listOf(3L), dao.observeAll().first().map { it.id })

        dao.upsert(listOf(target(3, 0, hidden = true)))
        assertTrue(dao.observeAll().first().single().hidden)
    }

    @Test
    fun historyReplaceAndClear() = runBlocking {
        val dao = db.historyDao()
        assertNull(dao.observeMeta().first())
        dao.replaceFirstPage(
            listOf(HistoryItemEntity(5, 0, "{}", "[]"), HistoryItemEntity(4, 1, "{}", "[]")),
            HistoryMetaEntity(nextCursor = "c", fetchedAtMillis = 1),
        )
        assertEquals(listOf(5L, 4L), dao.observeItems().first().map { it.id })
        assertEquals("c", dao.observeMeta().first()?.nextCursor)

        dao.replaceFirstPage(listOf(HistoryItemEntity(6, 0, "{}", "[]")), HistoryMetaEntity(nextCursor = null, fetchedAtMillis = 2))
        assertEquals(listOf(6L), dao.observeItems().first().map { it.id })
        assertNull(dao.observeMeta().first()?.nextCursor)

        dao.clear()
        assertTrue(dao.observeItems().first().isEmpty())
        assertNull(dao.observeMeta().first())
    }
}
