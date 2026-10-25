import datetime

from django.contrib.auth import get_user_model
from django.core.management.base import BaseCommand

from shop.models import Category, Product


class Command(BaseCommand):
    def handle(self, *args, **options):
        get_user_model().objects.create_superuser("admin", "admin@example.invalid", "admin-local-test-pass-123")
        cats = [Category.objects.create(name=n) for n in ("Audio", "Cables", "Displays", "Storage")]
        status = ["draft", "live", "retired"]
        for i in range(1, 48):
            Product.objects.create(
                name=f"Product {i:02d}",
                sku=f"SKU-{i:04d}",
                category=cats[i % 4],
                price=round(9.5 + i * 3.25, 2),
                in_stock=i % 3 != 0,
                status=status[i % 3],
                released=datetime.date(2024, 1, 1) + datetime.timedelta(days=i * 9),
            )
