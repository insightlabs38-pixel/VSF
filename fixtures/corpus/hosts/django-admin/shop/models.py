from django.db import models


class Category(models.Model):
    name = models.CharField(max_length=60, unique=True)

    class Meta:
        verbose_name_plural = "categories"

    def __str__(self):
        return self.name


class Product(models.Model):
    STATUS = [("draft", "Draft"), ("live", "Live"), ("retired", "Retired")]
    name = models.CharField(max_length=80)
    sku = models.CharField(max_length=20, unique=True)
    category = models.ForeignKey(Category, on_delete=models.PROTECT)
    price = models.DecimalField(max_digits=8, decimal_places=2)
    in_stock = models.BooleanField(default=True)
    status = models.CharField(max_length=10, choices=STATUS, default="draft")
    released = models.DateField(null=True, blank=True)
    manual = models.FileField(upload_to="manuals", blank=True)

    def __str__(self):
        return self.name


class Variant(models.Model):
    product = models.ForeignKey(Product, on_delete=models.CASCADE)
    label = models.CharField(max_length=40)
    extra_price = models.DecimalField(max_digits=6, decimal_places=2, default=0)
